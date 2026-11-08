package models

import "time"

const (
	StatusActive     = "ACTIVE"
	StatusOnLeave    = "ON_LEAVE"
	StatusTerminated = "TERMINATED"

	PayslipDraft = "DRAFT"
	PayslipPaid  = "PAID"

	LeavePending  = "PENDING"
	LeaveApproved = "APPROVED"
	LeaveRejected = "REJECTED"
)

type Department struct {
	ID          string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name        string     `gorm:"not null;uniqueIndex"`
	Description string     `gorm:"not null;default:''"`
	Location    string     `gorm:"not null;default:''"`
	Employees   []Employee `gorm:"constraint:OnDelete:RESTRICT"`
	CreatedAt   time.Time
	UpdatedAt   time.Time

	EmployeeCount int32   `gorm:"->;-:migration"`
	AverageSalary float64 `gorm:"->;-:migration"`
}

type WorkSchedule struct {
	ID               string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name             string     `gorm:"not null;uniqueIndex"`
	WorkDays         string     `gorm:"not null"`
	StartTime        string     `gorm:"type:char(5);not null"`
	EndTime          string     `gorm:"type:char(5);not null"`
	LateGraceMinutes int32      `gorm:"not null;default:0;check:chk_work_schedules_grace,late_grace_minutes BETWEEN 0 AND 240"`
	Employees        []Employee `gorm:"foreignKey:ScheduleID;constraint:OnDelete:SET NULL"`
	CreatedAt        time.Time
	UpdatedAt        time.Time

	EmployeeCount int32 `gorm:"->;-:migration"`
}

type Employee struct {
	ID           string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	DepartmentID string `gorm:"type:uuid;not null;index"`
	Department   Department
	ScheduleID   *string `gorm:"type:uuid;index"`
	Schedule     *WorkSchedule
	FirstName    string    `gorm:"not null"`
	LastName     string    `gorm:"not null;index"`
	Email        string    `gorm:"not null;uniqueIndex"`
	Phone        string    `gorm:"not null;default:''"`
	Position     string    `gorm:"not null;default:''"`
	Salary       float64   `gorm:"type:numeric(14,2);not null;default:0;check:chk_employees_salary,salary >= 0"`
	HireDate     time.Time `gorm:"type:date;not null"`
	Status       string    `gorm:"not null;default:'ACTIVE';index;check:chk_employees_status,status IN ('ACTIVE','ON_LEAVE','TERMINATED')"`

	Reviews        []PerformanceReview `gorm:"foreignKey:EmployeeID;constraint:OnDelete:CASCADE"`
	ReviewsWritten []PerformanceReview `gorm:"foreignKey:ReviewerID;constraint:OnDelete:SET NULL"`
	Attendance     []AttendanceRecord  `gorm:"constraint:OnDelete:CASCADE"`
	Payslips       []Payslip           `gorm:"constraint:OnDelete:CASCADE"`
	Leaves         []Leave             `gorm:"constraint:OnDelete:CASCADE"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type PerformanceReview struct {
	ID         string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	EmployeeID string    `gorm:"type:uuid;not null;uniqueIndex:idx_reviews_employee_period"`
	Employee   Employee  `gorm:"foreignKey:EmployeeID"`
	ReviewerID *string   `gorm:"type:uuid;index"`
	Reviewer   *Employee `gorm:"foreignKey:ReviewerID"`
	Period     string    `gorm:"not null;uniqueIndex:idx_reviews_employee_period"`
	Score      int32     `gorm:"not null;check:chk_reviews_score,score BETWEEN 1 AND 5"`
	Comment    string    `gorm:"not null;default:''"`
	Goals      string    `gorm:"not null;default:''"`
	ReviewDate time.Time `gorm:"type:date;not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type AttendanceRecord struct {
	ID          string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	EmployeeID  string    `gorm:"type:uuid;not null;uniqueIndex:idx_attendance_employee_date"`
	Date        time.Time `gorm:"type:date;not null;uniqueIndex:idx_attendance_employee_date;index"`
	CheckIn     string    `gorm:"type:char(5);not null"`
	CheckOut    *string   `gorm:"type:char(5)"`
	LateMinutes int32     `gorm:"not null;default:0"`
	Note        string    `gorm:"not null;default:''"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Payslip struct {
	ID         string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	EmployeeID string `gorm:"type:uuid;not null;uniqueIndex:idx_payslips_employee_period"`
	Employee   Employee
	Period     string  `gorm:"type:char(7);not null;uniqueIndex:idx_payslips_employee_period;index"`
	BaseAmount float64 `gorm:"type:numeric(14,2);not null"`
	Bonus      float64 `gorm:"type:numeric(14,2);not null;default:0"`
	Deductions float64 `gorm:"type:numeric(14,2);not null;default:0"`
	NetAmount  float64 `gorm:"type:numeric(14,2);not null;check:chk_payslips_net,net_amount >= 0"`
	Status     string  `gorm:"not null;default:'DRAFT';check:chk_payslips_status,status IN ('DRAFT','PAID')"`
	PaidAt     *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Leave struct {
	ID              string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	EmployeeID      string `gorm:"type:uuid;not null;index"`
	Employee        Employee
	Type            string    `gorm:"not null;check:chk_leaves_type,type IN ('VACATION','SICK_LEAVE','UNPAID_LEAVE')"`
	StartDate       time.Time `gorm:"type:date;not null"`
	EndDate         time.Time `gorm:"type:date;not null;check:chk_leaves_dates,end_date >= start_date"`
	Reason          string    `gorm:"not null;default:''"`
	Status          string    `gorm:"not null;default:'PENDING';index;check:chk_leaves_status,status IN ('PENDING','APPROVED','REJECTED')"`
	DecisionComment string    `gorm:"not null;default:''"`
	DecidedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
