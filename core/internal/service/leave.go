package service

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"gorm.io/gorm"

	"hr-service/core/internal/models"

	pb "hr-service/core/gen/hr/v1"
)

const maxLeaveDays = 366

type LeaveService struct {
	pb.UnimplementedLeaveServiceServer
	db *gorm.DB
}

func NewLeaveService(db *gorm.DB) *LeaveService {
	return &LeaveService{db: db}
}

func (s *LeaveService) CreateLeave(ctx context.Context, req *pb.CreateLeaveRequest) (*pb.Leave, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	l := models.Leave{EmployeeID: req.EmployeeId, Status: models.LeavePending}
	var err error
	if l.Type, err = enumName(int32(req.Type), "type", pb.LeaveType_name); err != nil {
		return nil, invalid("type is required: VACATION, SICK_LEAVE or UNPAID_LEAVE")
	}
	if l.StartDate, err = parseDate(req.StartDate, "start_date"); err != nil {
		return nil, err
	}
	if l.EndDate, err = parseDate(req.EndDate, "end_date"); err != nil {
		return nil, err
	}
	if l.EndDate.Before(l.StartDate) {
		return nil, invalid("end_date must not be before start_date")
	}
	if leaveDays(&l) > maxLeaveDays {
		return nil, invalid("a leave can be at most %d days long", maxLeaveDays)
	}
	if l.Reason, err = optionalText(req.Reason, "reason", 1000); err != nil {
		return nil, err
	}

	db := s.db.WithContext(ctx)
	e, err := loadEmployee(db, req.EmployeeId)
	if err != nil {
		return nil, err
	}
	if e.Status == models.StatusTerminated {
		return nil, conflict("terminated employees cannot request leave")
	}
	if l.StartDate.Before(e.HireDate) {
		return nil, invalid("start_date is before the employee's hire_date (%s)", formatDate(e.HireDate))
	}
	var overlap models.Leave
	err = db.Where("employee_id = ? AND status IN ? AND start_date <= ? AND end_date >= ?",
		e.ID, []string{models.LeavePending, models.LeaveApproved}, l.EndDate, l.StartDate).
		Order("start_date").Take(&overlap).Error
	if err == nil {
		return nil, conflict("overlaps with a %s leave from %s to %s",
			overlap.Status, formatDate(overlap.StartDate), formatDate(overlap.EndDate))
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dbErr(err)
	}

	if err := db.Omit("Employee").Create(&l).Error; err != nil {
		return nil, dbErr(err)
	}
	setHTTPStatus(ctx, 201)
	return s.load(ctx, l.ID)
}

func (s *LeaveService) ListEmployeeLeaves(ctx context.Context, req *pb.ListEmployeeLeavesRequest) (*pb.ListLeavesResponse, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	if err := mustExist(s.db.WithContext(ctx), &models.Employee{}, req.EmployeeId, "employee"); err != nil {
		return nil, err
	}
	return s.list(ctx, leaveFilter{employeeID: req.EmployeeId, status: req.Status}, req.Limit, req.Offset)
}

func (s *LeaveService) ListLeaves(ctx context.Context, req *pb.ListLeavesRequest) (*pb.ListLeavesResponse, error) {
	f := leaveFilter{status: req.Status, leaveType: req.Type}
	if req.DepartmentId != "" {
		if err := checkID(req.DepartmentId, "department_id"); err != nil {
			return nil, err
		}
		f.departmentID = req.DepartmentId
	}
	return s.list(ctx, f, req.Limit, req.Offset)
}

func (s *LeaveService) GetLeave(ctx context.Context, req *pb.GetLeaveRequest) (*pb.Leave, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	return s.load(ctx, req.Id)
}

func (s *LeaveService) ApproveLeave(ctx context.Context, req *pb.DecideLeaveRequest) (*pb.Leave, error) {
	return s.decide(ctx, req, models.LeaveApproved)
}

func (s *LeaveService) RejectLeave(ctx context.Context, req *pb.DecideLeaveRequest) (*pb.Leave, error) {
	return s.decide(ctx, req, models.LeaveRejected)
}

func (s *LeaveService) decide(ctx context.Context, req *pb.DecideLeaveRequest, to string) (*pb.Leave, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	comment, err := optionalText(req.Comment, "comment", 1000)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	res := db.Model(&models.Leave{}).Where("id = ? AND status = ?", req.Id, models.LeavePending).
		Updates(map[string]any{"status": to, "decision_comment": comment, "decided_at": time.Now().UTC()})
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		l, err := s.loadModel(db, req.Id)
		if err != nil {
			return nil, err
		}
		return nil, conflict("leave is already %s; only PENDING leaves can be approved or rejected", l.Status)
	}
	return s.load(ctx, req.Id)
}

func (s *LeaveService) DeleteLeave(ctx context.Context, req *pb.DeleteLeaveRequest) (*emptypb.Empty, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	res := db.Where("status = ?", models.LeavePending).Delete(&models.Leave{}, "id = ?", req.Id)
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		l, err := s.loadModel(db, req.Id)
		if err != nil {
			return nil, err
		}
		return nil, conflict("leave is already %s; only PENDING leaves can be withdrawn", l.Status)
	}
	setHTTPStatus(ctx, 204)
	return &emptypb.Empty{}, nil
}

type leaveFilter struct {
	employeeID, departmentID string
	status                   pb.LeaveStatus
	leaveType                pb.LeaveType
}

func (f leaveFilter) scope(db *gorm.DB) *gorm.DB {
	if f.employeeID != "" {
		db = db.Where("leaves.employee_id = ?", f.employeeID)
	}
	if f.departmentID != "" {
		db = db.Where("EXISTS (SELECT 1 FROM employees e WHERE e.id = leaves.employee_id AND e.department_id = ?)", f.departmentID)
	}
	if f.status != pb.LeaveStatus_LEAVE_STATUS_UNSPECIFIED {
		db = db.Where("leaves.status = ?", f.status.String())
	}
	if f.leaveType != pb.LeaveType_LEAVE_TYPE_UNSPECIFIED {
		db = db.Where("leaves.type = ?", f.leaveType.String())
	}
	return db
}

func (s *LeaveService) list(ctx context.Context, f leaveFilter, limit, offset int32) (*pb.ListLeavesResponse, error) {
	lim, off, err := paging(limit, offset)
	if err != nil {
		return nil, err
	}
	if _, ok := pb.LeaveStatus_name[int32(f.status)]; !ok {
		return nil, invalid("status has an invalid value")
	}
	if _, ok := pb.LeaveType_name[int32(f.leaveType)]; !ok {
		return nil, invalid("type has an invalid value")
	}
	db := s.db.WithContext(ctx)
	var total int64
	if err := db.Model(&models.Leave{}).Scopes(f.scope).Count(&total).Error; err != nil {
		return nil, dbErr(err)
	}
	var list []models.Leave
	if err := db.Preload("Employee").Scopes(f.scope).Order("leaves.start_date DESC, leaves.created_at DESC").
		Limit(lim).Offset(off).Find(&list).Error; err != nil {
		return nil, dbErr(err)
	}
	resp := &pb.ListLeavesResponse{Total: int32(total), Leaves: make([]*pb.Leave, 0, len(list))}
	for i := range list {
		resp.Leaves = append(resp.Leaves, toLeavePB(&list[i]))
	}
	return resp, nil
}

func (s *LeaveService) load(ctx context.Context, id string) (*pb.Leave, error) {
	l, err := s.loadModel(s.db.WithContext(ctx).Preload("Employee"), id)
	if err != nil {
		return nil, err
	}
	return toLeavePB(l), nil
}

func (s *LeaveService) loadModel(db *gorm.DB, id string) (*models.Leave, error) {
	var l models.Leave
	err := db.Take(&l, "leaves.id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, notFound("leave")
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return &l, nil
}

func leaveDays(l *models.Leave) int {
	return int(l.EndDate.Sub(l.StartDate).Hours()/24) + 1
}

func toLeavePB(l *models.Leave) *pb.Leave {
	return &pb.Leave{
		Id:              l.ID,
		EmployeeId:      l.EmployeeID,
		Employee:        employeeSummary(&l.Employee),
		Type:            pb.LeaveType(pb.LeaveType_value[l.Type]),
		StartDate:       formatDate(l.StartDate),
		EndDate:         formatDate(l.EndDate),
		Days:            int32(leaveDays(l)),
		Reason:          l.Reason,
		Status:          pb.LeaveStatus(pb.LeaveStatus_value[l.Status]),
		DecisionComment: l.DecisionComment,
		DecidedAt:       tsPtr(l.DecidedAt),
		CreatedAt:       ts(l.CreatedAt),
		UpdatedAt:       ts(l.UpdatedAt),
	}
}
