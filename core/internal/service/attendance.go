package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"gorm.io/gorm"

	"hr-service/core/internal/models"

	pb "hr-service/core/gen/hr/v1"
)

type AttendanceService struct {
	pb.UnimplementedAttendanceServiceServer
	db  *gorm.DB
	loc *time.Location
}

func NewAttendanceService(db *gorm.DB, loc *time.Location) *AttendanceService {
	return &AttendanceService{db: db, loc: loc}
}

func (s *AttendanceService) CheckIn(ctx context.Context, req *pb.CheckInRequest) (*pb.AttendanceRecord, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	note, err := optionalText(req.Note, "note", 500)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	e, err := loadEmployee(db, req.EmployeeId)
	if err != nil {
		return nil, err
	}
	date, now := today(s.loc), time.Now().In(s.loc).Format("15:04")
	if err := s.checkCanAttend(db, e, date); err != nil {
		return nil, err
	}
	var existing models.AttendanceRecord
	err = db.Take(&existing, "employee_id = ? AND date = ?", e.ID, date).Error
	if err == nil {
		return nil, conflict("employee already checked in today at %s", existing.CheckIn)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dbErr(err)
	}

	r := models.AttendanceRecord{
		EmployeeID:  e.ID,
		Date:        date,
		CheckIn:     now,
		LateMinutes: lateMinutes(e.Schedule, date, now),
		Note:        note,
	}
	if err := db.Create(&r).Error; err != nil {
		return nil, dbErr(err) // unique (employee_id, date) still guards against a concurrent check-in
	}
	setHTTPStatus(ctx, 201)
	return toAttendancePB(&r), nil
}

func (s *AttendanceService) CheckOut(ctx context.Context, req *pb.CheckOutRequest) (*pb.AttendanceRecord, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	if err := mustExist(db, &models.Employee{}, req.EmployeeId, "employee"); err != nil {
		return nil, err
	}
	var r models.AttendanceRecord
	err := db.Take(&r, "employee_id = ? AND date = ?", req.EmployeeId, today(s.loc)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, conflict("employee has not checked in today")
	}
	if err != nil {
		return nil, dbErr(err)
	}
	if r.CheckOut != nil {
		return nil, conflict("employee already checked out today at %s", *r.CheckOut)
	}
	now := time.Now().In(s.loc).Format("15:04")
	if now < r.CheckIn {
		now = r.CheckIn
	}
	if err := db.Model(&r).Update("check_out", now).Error; err != nil {
		return nil, dbErr(err)
	}
	r.CheckOut = &now
	return toAttendancePB(&r), nil
}

func (s *AttendanceService) CreateAttendance(ctx context.Context, req *pb.CreateAttendanceRequest) (*pb.AttendanceRecord, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	date, err := parseDate(req.Date, "date")
	if err != nil {
		return nil, err
	}
	if date.After(today(s.loc)) {
		return nil, invalid("date must not be in the future")
	}
	checkIn, inMin, err := parseClock(req.CheckIn, "check_in")
	if err != nil {
		return nil, err
	}
	var checkOut *string
	if strings.TrimSpace(req.CheckOut) != "" {
		out, outMin, err := parseClock(req.CheckOut, "check_out")
		if err != nil {
			return nil, err
		}
		if outMin <= inMin {
			return nil, invalid("check_out must be later than check_in")
		}
		checkOut = &out
	}
	note, err := optionalText(req.Note, "note", 500)
	if err != nil {
		return nil, err
	}

	db := s.db.WithContext(ctx)
	e, err := loadEmployee(db, req.EmployeeId)
	if err != nil {
		return nil, err
	}
	if date.Before(e.HireDate) {
		return nil, invalid("date is before the employee's hire_date (%s)", formatDate(e.HireDate))
	}
	if err := s.checkCanAttend(db, e, date); err != nil {
		return nil, err
	}
	r := models.AttendanceRecord{
		EmployeeID:  e.ID,
		Date:        date,
		CheckIn:     checkIn,
		CheckOut:    checkOut,
		LateMinutes: lateMinutes(e.Schedule, date, checkIn),
		Note:        note,
	}
	if err := db.Create(&r).Error; err != nil {
		return nil, dbErr(err)
	}
	setHTTPStatus(ctx, 201)
	return toAttendancePB(&r), nil
}

func (s *AttendanceService) ListEmployeeAttendance(ctx context.Context, req *pb.ListEmployeeAttendanceRequest) (*pb.ListAttendanceResponse, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	limit, offset, err := paging(req.Limit, req.Offset)
	if err != nil {
		return nil, err
	}
	var from, to *time.Time
	if req.DateFrom != "" {
		d, err := parseDate(req.DateFrom, "date_from")
		if err != nil {
			return nil, err
		}
		from = &d
	}
	if req.DateTo != "" {
		d, err := parseDate(req.DateTo, "date_to")
		if err != nil {
			return nil, err
		}
		to = &d
	}
	if from != nil && to != nil && from.After(*to) {
		return nil, invalid("date_from must not be after date_to")
	}

	db := s.db.WithContext(ctx)
	if err := mustExist(db, &models.Employee{}, req.EmployeeId, "employee"); err != nil {
		return nil, err
	}
	filter := func(db *gorm.DB) *gorm.DB {
		db = db.Where("employee_id = ?", req.EmployeeId)
		if from != nil {
			db = db.Where("date >= ?", *from)
		}
		if to != nil {
			db = db.Where("date <= ?", *to)
		}
		return db
	}

	var sum struct {
		Total       int64
		Late        int32
		Worked      int32
		LateMinutes int32
	}
	if err := db.Model(&models.AttendanceRecord{}).Scopes(filter).Select(`
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE late_minutes > 0) AS late,
			COALESCE(SUM(EXTRACT(EPOCH FROM (check_out::time - check_in::time)) / 60), 0)::int AS worked,
			COALESCE(SUM(late_minutes), 0)::int AS late_minutes`).
		Scan(&sum).Error; err != nil {
		return nil, dbErr(err)
	}
	var list []models.AttendanceRecord
	if err := db.Scopes(filter).Order("date DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		return nil, dbErr(err)
	}

	resp := &pb.ListAttendanceResponse{
		Total:   int32(sum.Total),
		Records: make([]*pb.AttendanceRecord, 0, len(list)),
		Summary: &pb.AttendanceSummary{
			DaysPresent:        int32(sum.Total),
			DaysLate:           sum.Late,
			TotalWorkedMinutes: sum.Worked,
			TotalLateMinutes:   sum.LateMinutes,
		},
	}
	for i := range list {
		resp.Records = append(resp.Records, toAttendancePB(&list[i]))
	}
	return resp, nil
}

func (s *AttendanceService) DeleteAttendance(ctx context.Context, req *pb.DeleteAttendanceRequest) (*emptypb.Empty, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	res := s.db.WithContext(ctx).Delete(&models.AttendanceRecord{}, "id = ?", req.Id)
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, notFound("attendance record")
	}
	setHTTPStatus(ctx, 204)
	return &emptypb.Empty{}, nil
}

func (s *AttendanceService) GetDepartmentAttendance(ctx context.Context, req *pb.GetDepartmentAttendanceRequest) (*pb.DepartmentAttendance, error) {
	if err := checkID(req.DepartmentId, "department_id"); err != nil {
		return nil, err
	}
	date := today(s.loc)
	if req.Date != "" {
		d, err := parseDate(req.Date, "date")
		if err != nil {
			return nil, err
		}
		date = d
	}
	db := s.db.WithContext(ctx)
	if err := mustExist(db, &models.Department{}, req.DepartmentId, "department"); err != nil {
		return nil, err
	}

	var staff []models.Employee
	if err := db.Preload("Schedule").
		Where("department_id = ? AND status <> ? AND hire_date <= ?", req.DepartmentId, models.StatusTerminated, date).
		Order("last_name, first_name").Find(&staff).Error; err != nil {
		return nil, dbErr(err)
	}
	ids := make([]string, len(staff))
	for i, e := range staff {
		ids[i] = e.ID
	}

	records := map[string]models.AttendanceRecord{}
	onLeave := map[string]bool{}
	if len(ids) > 0 {
		var rs []models.AttendanceRecord
		if err := db.Where("employee_id IN ? AND date = ?", ids, date).Find(&rs).Error; err != nil {
			return nil, dbErr(err)
		}
		for _, r := range rs {
			records[r.EmployeeID] = r
		}
		var leaveIDs []string
		if err := db.Model(&models.Leave{}).
			Where("employee_id IN ? AND status = ? AND start_date <= ? AND end_date >= ?", ids, models.LeaveApproved, date, date).
			Pluck("employee_id", &leaveIDs).Error; err != nil {
			return nil, dbErr(err)
		}
		for _, id := range leaveIDs {
			onLeave[id] = true
		}
	}

	resp := &pb.DepartmentAttendance{DepartmentId: req.DepartmentId, Date: formatDate(date)}
	for i := range staff {
		e := &staff[i]
		entry := &pb.DepartmentAttendanceEntry{Employee: employeeSummary(e)}
		r, present := records[e.ID]
		switch {
		case present:
			entry.CheckIn, entry.CheckOut, entry.LateMinutes = r.CheckIn, derefStr(r.CheckOut), r.LateMinutes
			if r.LateMinutes > 0 {
				entry.Status = pb.PresenceStatus_LATE
				resp.LateCount++
			} else {
				entry.Status = pb.PresenceStatus_PRESENT
			}
			resp.PresentCount++
		case onLeave[e.ID] || e.Status == models.StatusOnLeave:
			entry.Status = pb.PresenceStatus_LEAVE
			resp.LeaveCount++
		case !isWorkDay(e.Schedule, date):
			entry.Status = pb.PresenceStatus_DAY_OFF
			resp.DayOffCount++
		default:
			entry.Status = pb.PresenceStatus_ABSENT
			resp.AbsentCount++
		}
		resp.Entries = append(resp.Entries, entry)
	}
	return resp, nil
}

func (s *AttendanceService) checkCanAttend(db *gorm.DB, e *models.Employee, date time.Time) error {
	if e.Status == models.StatusTerminated {
		return conflict("terminated employees cannot have attendance records")
	}
	var n int64
	if err := db.Model(&models.Leave{}).
		Where("employee_id = ? AND status = ? AND start_date <= ? AND end_date >= ?", e.ID, models.LeaveApproved, date, date).
		Count(&n).Error; err != nil {
		return dbErr(err)
	}
	if n > 0 {
		return conflict("employee is on an approved leave on %s", formatDate(date))
	}
	return nil
}

func toAttendancePB(r *models.AttendanceRecord) *pb.AttendanceRecord {
	out := &pb.AttendanceRecord{
		Id:          r.ID,
		EmployeeId:  r.EmployeeID,
		Date:        formatDate(r.Date),
		CheckIn:     r.CheckIn,
		CheckOut:    derefStr(r.CheckOut),
		LateMinutes: r.LateMinutes,
		IsLate:      r.LateMinutes > 0,
		Note:        r.Note,
		CreatedAt:   ts(r.CreatedAt),
		UpdatedAt:   ts(r.UpdatedAt),
	}
	if r.CheckOut != nil {
		out.WorkedMinutes = int32(clockMinutes(*r.CheckOut) - clockMinutes(r.CheckIn))
	}
	return out
}
