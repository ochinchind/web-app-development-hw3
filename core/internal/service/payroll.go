package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hr-service/core/internal/models"

	pb "hr-service/core/gen/hr/v1"
)

var payPeriodRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

type PayrollService struct {
	pb.UnimplementedPayrollServiceServer
	db  *gorm.DB
	loc *time.Location
}

func NewPayrollService(db *gorm.DB, loc *time.Location) *PayrollService {
	return &PayrollService{db: db, loc: loc}
}

func (s *PayrollService) CreatePayslip(ctx context.Context, req *pb.CreatePayslipRequest) (*pb.Payslip, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	period, start, err := s.parsePeriod(req.Period)
	if err != nil {
		return nil, err
	}
	bonus, err := money(req.Bonus, "bonus")
	if err != nil {
		return nil, err
	}
	deductions, err := money(req.Deductions, "deductions")
	if err != nil {
		return nil, err
	}

	db := s.db.WithContext(ctx)
	e, err := loadEmployee(db, req.EmployeeId)
	if err != nil {
		return nil, err
	}
	if e.Status == models.StatusTerminated {
		return nil, conflict("cannot create a payslip for a terminated employee")
	}
	if start.AddDate(0, 1, -1).Before(e.HireDate) {
		return nil, invalid("period %s is before the employee's hire_date (%s)", period, formatDate(e.HireDate))
	}
	base := e.Salary
	if req.BaseAmount != nil {
		if base, err = money(*req.BaseAmount, "base_amount"); err != nil {
			return nil, err
		}
	} else if base == 0 {
		return nil, invalid("the employee has no salary set; provide base_amount")
	}
	p := models.Payslip{EmployeeID: e.ID, Period: period, BaseAmount: base, Bonus: bonus, Deductions: deductions}
	if err := computeNet(&p); err != nil {
		return nil, err
	}
	if err := db.Omit("Employee").Create(&p).Error; err != nil {
		return nil, dbErr(err)
	}
	setHTTPStatus(ctx, 201)
	return s.load(ctx, p.ID)
}

func (s *PayrollService) ListEmployeePayslips(ctx context.Context, req *pb.ListEmployeePayslipsRequest) (*pb.ListPayslipsResponse, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	limit, offset, err := paging(req.Limit, req.Offset)
	if err != nil {
		return nil, err
	}
	if _, ok := pb.PayslipStatus_name[int32(req.Status)]; !ok {
		return nil, invalid("status has an invalid value")
	}
	db := s.db.WithContext(ctx)
	if err := mustExist(db, &models.Employee{}, req.EmployeeId, "employee"); err != nil {
		return nil, err
	}
	filter := func(db *gorm.DB) *gorm.DB {
		db = db.Where("employee_id = ?", req.EmployeeId)
		if req.Status != pb.PayslipStatus_PAYSLIP_STATUS_UNSPECIFIED {
			db = db.Where("status = ?", req.Status.String())
		}
		return db
	}
	var total int64
	if err := db.Model(&models.Payslip{}).Scopes(filter).Count(&total).Error; err != nil {
		return nil, dbErr(err)
	}
	var list []models.Payslip
	if err := db.Preload("Employee").Scopes(filter).Order("period DESC").
		Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		return nil, dbErr(err)
	}
	resp := &pb.ListPayslipsResponse{Total: int32(total), Payslips: make([]*pb.Payslip, 0, len(list))}
	for i := range list {
		resp.Payslips = append(resp.Payslips, toPayslipPB(&list[i]))
	}
	return resp, nil
}

func (s *PayrollService) GetPayslip(ctx context.Context, req *pb.GetPayslipRequest) (*pb.Payslip, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	return s.load(ctx, req.Id)
}

func (s *PayrollService) PatchPayslip(ctx context.Context, req *pb.PatchPayslipRequest) (*pb.Payslip, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	if req.BaseAmount == nil && req.Bonus == nil && req.Deductions == nil {
		return nil, invalid("provide at least one field to change: base_amount, bonus, deductions")
	}
	db := s.db.WithContext(ctx)
	p, err := s.loadModel(db, req.Id)
	if err != nil {
		return nil, err
	}
	if p.Status != models.PayslipDraft {
		return nil, conflict("payslip is already PAID and can no longer be changed")
	}
	if req.BaseAmount != nil {
		if p.BaseAmount, err = money(*req.BaseAmount, "base_amount"); err != nil {
			return nil, err
		}
	}
	if req.Bonus != nil {
		if p.Bonus, err = money(*req.Bonus, "bonus"); err != nil {
			return nil, err
		}
	}
	if req.Deductions != nil {
		if p.Deductions, err = money(*req.Deductions, "deductions"); err != nil {
			return nil, err
		}
	}
	if err := computeNet(p); err != nil {
		return nil, err
	}
	res := db.Model(&models.Payslip{}).Where("id = ? AND status = ?", p.ID, models.PayslipDraft).Updates(map[string]any{
		"base_amount": p.BaseAmount, "bonus": p.Bonus, "deductions": p.Deductions, "net_amount": p.NetAmount,
	})
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, conflict("payslip is already PAID and can no longer be changed")
	}
	return s.load(ctx, p.ID)
}

func (s *PayrollService) PayPayslip(ctx context.Context, req *pb.PayPayslipRequest) (*pb.Payslip, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	res := db.Model(&models.Payslip{}).Where("id = ? AND status = ?", req.Id, models.PayslipDraft).
		Updates(map[string]any{"status": models.PayslipPaid, "paid_at": time.Now().UTC()})
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		p, err := s.loadModel(db, req.Id)
		if err != nil {
			return nil, err
		}
		return nil, conflict("payslip was already paid at %s", p.PaidAt.Format(time.RFC3339))
	}
	return s.load(ctx, req.Id)
}

func (s *PayrollService) DeletePayslip(ctx context.Context, req *pb.DeletePayslipRequest) (*emptypb.Empty, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	res := db.Where("status = ?", models.PayslipDraft).Delete(&models.Payslip{}, "id = ?", req.Id)
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		if _, err := s.loadModel(db, req.Id); err != nil {
			return nil, err
		}
		return nil, conflict("paid payslips cannot be deleted")
	}
	setHTTPStatus(ctx, 204)
	return &emptypb.Empty{}, nil
}

func (s *PayrollService) RunDepartmentPayroll(ctx context.Context, req *pb.RunDepartmentPayrollRequest) (*pb.DepartmentPayroll, error) {
	if err := checkID(req.DepartmentId, "department_id"); err != nil {
		return nil, err
	}
	period, start, err := s.parsePeriod(req.Period)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	if err := mustExist(db, &models.Department{}, req.DepartmentId, "department"); err != nil {
		return nil, err
	}

	var staff []models.Employee
	if err := db.Where("department_id = ? AND status <> ? AND salary > 0 AND hire_date <= ?",
		req.DepartmentId, models.StatusTerminated, start.AddDate(0, 1, -1)).
		Where("NOT EXISTS (SELECT 1 FROM payslips p WHERE p.employee_id = employees.id AND p.period = ?)", period).
		Find(&staff).Error; err != nil {
		return nil, dbErr(err)
	}
	var created int64
	if len(staff) > 0 {
		slips := make([]models.Payslip, len(staff))
		for i, e := range staff {
			slips[i] = models.Payslip{EmployeeID: e.ID, Period: period, BaseAmount: e.Salary, NetAmount: e.Salary}
		}
		// ON CONFLICT DO NOTHING: a payslip created concurrently for the same month is simply skipped
		res := db.Omit("Employee").Clauses(clause.OnConflict{DoNothing: true}).Create(&slips)
		if res.Error != nil {
			return nil, dbErr(res.Error)
		}
		created = res.RowsAffected
	}

	summary, err := s.departmentPayroll(db, req.DepartmentId, period)
	if err != nil {
		return nil, err
	}
	summary.CreatedCount = int32(created)
	if created > 0 {
		setHTTPStatus(ctx, 201)
	}
	return summary, nil
}

func (s *PayrollService) GetDepartmentPayroll(ctx context.Context, req *pb.GetDepartmentPayrollRequest) (*pb.DepartmentPayroll, error) {
	if err := checkID(req.DepartmentId, "department_id"); err != nil {
		return nil, err
	}
	period := time.Now().In(s.loc).Format("2006-01")
	if req.Period != "" {
		p, _, err := s.parsePeriod(req.Period)
		if err != nil {
			return nil, err
		}
		period = p
	}
	db := s.db.WithContext(ctx)
	if err := mustExist(db, &models.Department{}, req.DepartmentId, "department"); err != nil {
		return nil, err
	}
	return s.departmentPayroll(db, req.DepartmentId, period)
}

func (s *PayrollService) departmentPayroll(db *gorm.DB, departmentID, period string) (*pb.DepartmentPayroll, error) {
	var slips []models.Payslip
	if err := db.Preload("Employee").
		Joins("JOIN employees ON employees.id = payslips.employee_id").
		Where("employees.department_id = ? AND payslips.period = ?", departmentID, period).
		Order("employees.last_name, employees.first_name").
		Find(&slips).Error; err != nil {
		return nil, dbErr(err)
	}
	out := &pb.DepartmentPayroll{DepartmentId: departmentID, Period: period, Payslips: make([]*pb.Payslip, 0, len(slips))}
	for i := range slips {
		p := &slips[i]
		out.PayslipCount++
		if p.Status == models.PayslipPaid {
			out.PaidCount++
		} else {
			out.DraftCount++
		}
		out.TotalBase += p.BaseAmount
		out.TotalBonus += p.Bonus
		out.TotalDeductions += p.Deductions
		out.TotalNet += p.NetAmount
		out.Payslips = append(out.Payslips, toPayslipPB(p))
	}
	out.TotalBase, out.TotalBonus = round2(out.TotalBase), round2(out.TotalBonus)
	out.TotalDeductions, out.TotalNet = round2(out.TotalDeductions), round2(out.TotalNet)
	return out, nil
}

func (s *PayrollService) load(ctx context.Context, id string) (*pb.Payslip, error) {
	p, err := s.loadModel(s.db.WithContext(ctx).Preload("Employee"), id)
	if err != nil {
		return nil, err
	}
	return toPayslipPB(p), nil
}

func (s *PayrollService) loadModel(db *gorm.DB, id string) (*models.Payslip, error) {
	var p models.Payslip
	err := db.Take(&p, "payslips.id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, notFound("payslip")
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return &p, nil
}

func (s *PayrollService) parsePeriod(v string) (string, time.Time, error) {
	v = strings.TrimSpace(v)
	if !payPeriodRe.MatchString(v) {
		return "", time.Time{}, invalid("period must be a month in the format YYYY-MM")
	}
	start, _ := time.Parse("2006-01", v)
	if start.After(today(s.loc)) {
		return "", time.Time{}, invalid("period must not be in the future")
	}
	return v, start, nil
}

func computeNet(p *models.Payslip) error {
	p.NetAmount = round2(p.BaseAmount + p.Bonus - p.Deductions)
	if p.NetAmount < 0 {
		return invalid("deductions must not exceed base_amount + bonus")
	}
	return nil
}

func toPayslipPB(p *models.Payslip) *pb.Payslip {
	return &pb.Payslip{
		Id:         p.ID,
		EmployeeId: p.EmployeeID,
		Employee:   employeeSummary(&p.Employee),
		Period:     p.Period,
		BaseAmount: p.BaseAmount,
		Bonus:      p.Bonus,
		Deductions: p.Deductions,
		NetAmount:  p.NetAmount,
		Status:     pb.PayslipStatus(pb.PayslipStatus_value[p.Status]),
		PaidAt:     tsPtr(p.PaidAt),
		CreatedAt:  ts(p.CreatedAt),
		UpdatedAt:  ts(p.UpdatedAt),
	}
}
