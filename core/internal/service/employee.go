package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"gorm.io/gorm"

	"hr-service/core/internal/models"

	pb "hr-service/core/gen/hr/v1"
)

var (
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phoneRe = regexp.MustCompile(`^\+?[0-9]{7,15}$`)
)

type EmployeeService struct {
	pb.UnimplementedEmployeeServiceServer
	db  *gorm.DB
	loc *time.Location
}

func NewEmployeeService(db *gorm.DB, loc *time.Location) *EmployeeService {
	return &EmployeeService{db: db, loc: loc}
}

func (s *EmployeeService) employees(db *gorm.DB) *gorm.DB {
	return db.Model(&models.Employee{}).Preload("Department").Preload("Schedule")
}

type employeeInput struct {
	departmentID, firstName, lastName, email, phone, position, hireDate, scheduleID string
	salary                                                                          float64
	status                                                                          pb.EmployeeStatus
}

func (s *EmployeeService) CreateEmployee(ctx context.Context, req *pb.CreateEmployeeRequest) (*pb.Employee, error) {
	e, err := s.validate(employeeInput{
		departmentID: req.DepartmentId, firstName: req.FirstName, lastName: req.LastName, email: req.Email,
		phone: req.Phone, position: req.Position, hireDate: req.HireDate, scheduleID: req.ScheduleId,
		salary: req.Salary, status: req.Status,
	})
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	if err := checkEmployeeRefs(db, &e.DepartmentID, e.ScheduleID); err != nil {
		return nil, err
	}
	if err := db.Omit("Department", "Schedule").Create(&e).Error; err != nil {
		return nil, dbErr(err)
	}
	setHTTPStatus(ctx, 201)
	return s.load(ctx, e.ID)
}

func (s *EmployeeService) ListEmployees(ctx context.Context, req *pb.ListEmployeesRequest) (*pb.ListEmployeesResponse, error) {
	f := employeeFilter{query: strings.TrimSpace(req.Q), status: req.Status}
	if req.DepartmentId != "" {
		if err := checkID(req.DepartmentId, "department_id"); err != nil {
			return nil, err
		}
		f.departmentID = req.DepartmentId
	}
	return s.list(ctx, f, req.Limit, req.Offset)
}

func (s *EmployeeService) GetEmployee(ctx context.Context, req *pb.GetEmployeeRequest) (*pb.Employee, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	return s.load(ctx, req.Id)
}

func (s *EmployeeService) UpdateEmployee(ctx context.Context, req *pb.UpdateEmployeeRequest) (*pb.Employee, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	e, err := s.validate(employeeInput{
		departmentID: req.DepartmentId, firstName: req.FirstName, lastName: req.LastName, email: req.Email,
		phone: req.Phone, position: req.Position, hireDate: req.HireDate, scheduleID: req.ScheduleId,
		salary: req.Salary, status: req.Status,
	})
	if err != nil {
		return nil, err
	}
	return s.apply(ctx, req.Id, map[string]any{
		"department_id": e.DepartmentID,
		"schedule_id":   e.ScheduleID,
		"first_name":    e.FirstName,
		"last_name":     e.LastName,
		"email":         e.Email,
		"phone":         e.Phone,
		"position":      e.Position,
		"salary":        e.Salary,
		"hire_date":     e.HireDate,
		"status":        e.Status,
	})
}

func (s *EmployeeService) PatchEmployee(ctx context.Context, req *pb.PatchEmployeeRequest) (*pb.Employee, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	changes := map[string]any{}
	var err error
	if req.DepartmentId != nil {
		if err := checkID(*req.DepartmentId, "department_id"); err != nil {
			return nil, err
		}
		changes["department_id"] = *req.DepartmentId
	}
	if req.ScheduleId != nil {
		id, err := optionalID(*req.ScheduleId, "schedule_id")
		if err != nil {
			return nil, err
		}
		changes["schedule_id"] = id
	}
	text := func(p *string, column string, maxLen int, required bool) {
		if p == nil || err != nil {
			return
		}
		var v string
		if required {
			v, err = requiredText(*p, column, maxLen)
		} else {
			v, err = optionalText(*p, column, maxLen)
		}
		changes[column] = v
	}
	text(req.FirstName, "first_name", 100, true)
	text(req.LastName, "last_name", 100, true)
	text(req.Position, "position", 100, false)
	if err != nil {
		return nil, err
	}
	if req.Email != nil {
		if changes["email"], err = normalizeEmail(*req.Email); err != nil {
			return nil, err
		}
	}
	if req.Phone != nil {
		if changes["phone"], err = normalizePhone(*req.Phone); err != nil {
			return nil, err
		}
	}
	if req.Salary != nil {
		if changes["salary"], err = money(*req.Salary, "salary"); err != nil {
			return nil, err
		}
	}
	if req.HireDate != nil {
		if changes["hire_date"], err = s.hireDate(*req.HireDate); err != nil {
			return nil, err
		}
	}
	if req.Status != nil {
		if changes["status"], err = enumName(int32(*req.Status), "status", pb.EmployeeStatus_name); err != nil {
			return nil, err
		}
	}
	if len(changes) == 0 {
		return nil, invalid("provide at least one field to change")
	}
	return s.apply(ctx, req.Id, changes)
}

func (s *EmployeeService) DeleteEmployee(ctx context.Context, req *pb.DeleteEmployeeRequest) (*emptypb.Empty, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	res := s.db.WithContext(ctx).Delete(&models.Employee{}, "id = ?", req.Id)
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, notFound("employee")
	}
	setHTTPStatus(ctx, 204)
	return &emptypb.Empty{}, nil
}

func (s *EmployeeService) ListDepartmentEmployees(ctx context.Context, req *pb.ListDepartmentEmployeesRequest) (*pb.ListEmployeesResponse, error) {
	if err := checkID(req.DepartmentId, "department_id"); err != nil {
		return nil, err
	}
	if err := mustExist(s.db.WithContext(ctx), &models.Department{}, req.DepartmentId, "department"); err != nil {
		return nil, err
	}
	return s.list(ctx, employeeFilter{departmentID: req.DepartmentId, status: req.Status}, req.Limit, req.Offset)
}

func (s *EmployeeService) ListScheduleEmployees(ctx context.Context, req *pb.ListScheduleEmployeesRequest) (*pb.ListEmployeesResponse, error) {
	if err := checkID(req.ScheduleId, "schedule_id"); err != nil {
		return nil, err
	}
	if err := mustExist(s.db.WithContext(ctx), &models.WorkSchedule{}, req.ScheduleId, "schedule"); err != nil {
		return nil, err
	}
	return s.list(ctx, employeeFilter{scheduleID: req.ScheduleId}, req.Limit, req.Offset)
}

func (s *EmployeeService) apply(ctx context.Context, id string, changes map[string]any) (*pb.Employee, error) {
	db := s.db.WithContext(ctx)
	var deptID, schedID *string
	if v, ok := changes["department_id"].(string); ok {
		deptID = &v
	}
	if v, ok := changes["schedule_id"].(*string); ok {
		schedID = v
	}
	if err := checkEmployeeRefs(db, deptID, schedID); err != nil {
		return nil, err
	}
	res := db.Model(&models.Employee{ID: id}).Updates(changes)
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, notFound("employee")
	}
	return s.load(ctx, id)
}

func (s *EmployeeService) load(ctx context.Context, id string) (*pb.Employee, error) {
	var e models.Employee
	err := s.employees(s.db.WithContext(ctx)).Take(&e, "employees.id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, notFound("employee")
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return toEmployeePB(&e), nil
}

type employeeFilter struct {
	query, departmentID, scheduleID string
	status                          pb.EmployeeStatus
}

func (f employeeFilter) scope(db *gorm.DB) *gorm.DB {
	if f.query != "" {
		p := likePattern(f.query)
		db = db.Where("(employees.first_name || ' ' || employees.last_name) ILIKE ? OR employees.email ILIKE ? OR employees.position ILIKE ?", p, p, p)
	}
	if f.departmentID != "" {
		db = db.Where("employees.department_id = ?", f.departmentID)
	}
	if f.scheduleID != "" {
		db = db.Where("employees.schedule_id = ?", f.scheduleID)
	}
	if f.status != pb.EmployeeStatus_EMPLOYEE_STATUS_UNSPECIFIED {
		db = db.Where("employees.status = ?", f.status.String())
	}
	return db
}

func (s *EmployeeService) list(ctx context.Context, f employeeFilter, limit, offset int32) (*pb.ListEmployeesResponse, error) {
	lim, off, err := paging(limit, offset)
	if err != nil {
		return nil, err
	}
	if _, ok := pb.EmployeeStatus_name[int32(f.status)]; !ok {
		return nil, invalid("status has an invalid value")
	}
	db := s.db.WithContext(ctx)
	var total int64
	if err := db.Model(&models.Employee{}).Scopes(f.scope).Count(&total).Error; err != nil {
		return nil, dbErr(err)
	}
	var list []models.Employee
	if err := s.employees(db).Scopes(f.scope).Order("employees.last_name, employees.first_name, employees.id").
		Limit(lim).Offset(off).Find(&list).Error; err != nil {
		return nil, dbErr(err)
	}
	resp := &pb.ListEmployeesResponse{Total: int32(total), Employees: make([]*pb.Employee, 0, len(list))}
	for i := range list {
		resp.Employees = append(resp.Employees, toEmployeePB(&list[i]))
	}
	return resp, nil
}

func (s *EmployeeService) validate(in employeeInput) (models.Employee, error) {
	var (
		e   models.Employee
		err error
	)
	if err = checkID(in.departmentID, "department_id"); err != nil {
		return e, err
	}
	e.DepartmentID = in.departmentID
	if e.ScheduleID, err = optionalID(in.scheduleID, "schedule_id"); err != nil {
		return e, err
	}
	if e.FirstName, err = requiredText(in.firstName, "first_name", 100); err != nil {
		return e, err
	}
	if e.LastName, err = requiredText(in.lastName, "last_name", 100); err != nil {
		return e, err
	}
	if e.Email, err = normalizeEmail(in.email); err != nil {
		return e, err
	}
	if e.Phone, err = normalizePhone(in.phone); err != nil {
		return e, err
	}
	if e.Position, err = optionalText(in.position, "position", 100); err != nil {
		return e, err
	}
	if e.Salary, err = money(in.salary, "salary"); err != nil {
		return e, err
	}
	if e.HireDate, err = s.hireDate(in.hireDate); err != nil {
		return e, err
	}
	e.Status = models.StatusActive
	if in.status != pb.EmployeeStatus_EMPLOYEE_STATUS_UNSPECIFIED {
		if e.Status, err = enumName(int32(in.status), "status", pb.EmployeeStatus_name); err != nil {
			return e, err
		}
	}
	return e, nil
}

// hireDate defaults to today and may be at most one year in the future (accepted job offers).
func (s *EmployeeService) hireDate(v string) (time.Time, error) {
	now := today(s.loc)
	if strings.TrimSpace(v) == "" {
		return now, nil
	}
	d, err := parseDate(v, "hire_date")
	if err != nil {
		return d, err
	}
	if d.After(now.AddDate(1, 0, 0)) {
		return d, invalid("hire_date must not be more than one year in the future")
	}
	return d, nil
}

// checkEmployeeRefs verifies that a referenced department / schedule exist (nil = not being set).
func checkEmployeeRefs(db *gorm.DB, departmentID, scheduleID *string) error {
	if departmentID != nil {
		ok, err := exists(db, &models.Department{}, *departmentID)
		if err != nil {
			return dbErr(err)
		}
		if !ok {
			return invalid("department_id does not reference an existing department")
		}
	}
	if scheduleID != nil {
		ok, err := exists(db, &models.WorkSchedule{}, *scheduleID)
		if err != nil {
			return dbErr(err)
		}
		if !ok {
			return invalid("schedule_id does not reference an existing schedule")
		}
	}
	return nil
}

func normalizeEmail(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "", invalid("email is required")
	}
	if len(v) > 200 || !emailRe.MatchString(v) {
		return "", invalid("email is not a valid email address")
	}
	return v, nil
}

func normalizePhone(v string) (string, error) {
	v = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(v))
	if v != "" && !phoneRe.MatchString(v) {
		return "", invalid("phone must be 7-15 digits, optionally starting with +")
	}
	return v, nil
}

func toEmployeePB(e *models.Employee) *pb.Employee {
	out := &pb.Employee{
		Id:           e.ID,
		FirstName:    e.FirstName,
		LastName:     e.LastName,
		FullName:     fullName(e),
		Email:        e.Email,
		Phone:        e.Phone,
		Position:     e.Position,
		DepartmentId: e.DepartmentID,
		Department:   &pb.DepartmentSummary{Id: e.Department.ID, Name: e.Department.Name},
		ScheduleId:   derefStr(e.ScheduleID),
		Salary:       e.Salary,
		HireDate:     formatDate(e.HireDate),
		Status:       pb.EmployeeStatus(pb.EmployeeStatus_value[e.Status]),
		CreatedAt:    ts(e.CreatedAt),
		UpdatedAt:    ts(e.UpdatedAt),
	}
	if e.Schedule != nil {
		out.Schedule = &pb.ScheduleSummary{Id: e.Schedule.ID, Name: e.Schedule.Name}
	}
	return out
}
