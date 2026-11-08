package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"

	"hr-service/core/internal/models"

	pb "hr-service/core/gen/hr/v1"
)

const (
	defaultLimit = 20
	maxLimit     = 100
	dateLayout   = "2006-01-02"
	maxMoney     = 1_000_000_000
)

const HTTPStatusHeader = "x-http-status"

func setHTTPStatus(ctx context.Context, code int) {
	_ = grpc.SetHeader(ctx, metadata.Pairs(HTTPStatusHeader, strconv.Itoa(code)))
}

func ConflictStatusInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
	resp, err := next(ctx, req)
	if status.Code(err) == codes.FailedPrecondition {
		setHTTPStatus(ctx, 409)
	}
	return resp, err
}

func invalid(format string, args ...any) error {
	return status.Errorf(codes.InvalidArgument, format, args...)
}

func notFound(entity string) error {
	return status.Errorf(codes.NotFound, "%s not found", entity)
}

func conflict(format string, args ...any) error {
	return status.Errorf(codes.FailedPrecondition, format, args...)
}

var uniqueMessages = map[string]string{
	"idx_departments_name":         "a department with this name already exists",
	"idx_work_schedules_name":      "a schedule with this name already exists",
	"idx_employees_email":          "an employee with this email already exists",
	"idx_reviews_employee_period":  "this employee already has a performance review for this period",
	"idx_attendance_employee_date": "this employee already has an attendance record for this date",
	"idx_payslips_employee_period": "this employee already has a payslip for this period",
}

func dbErr(err error) error {
	if _, ok := status.FromError(err); ok {
		return err // already a gRPC status (e.g. returned from inside a transaction)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			msg, ok := uniqueMessages[pgErr.ConstraintName]
			if !ok {
				msg = "already exists: " + pgErr.ConstraintName
			}
			return status.Error(codes.AlreadyExists, msg)
		case "23503":
			return conflict("operation violates a relationship (%s)", pgErr.ConstraintName)
		case "23514":
			return invalid("value violates constraint %s", pgErr.ConstraintName)
		}
	}
	log.Printf("database error: %v", err)
	return status.Error(codes.Internal, "internal database error")
}

func checkID(id, field string) error {
	if _, err := uuid.Parse(id); err != nil {
		return invalid("%s must be a valid UUID", field)
	}
	return nil
}

func optionalID(id, field string) (*string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	if err := checkID(id, field); err != nil {
		return nil, err
	}
	return &id, nil
}

func requiredText(value, field string, maxLen int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", invalid("%s is required", field)
	}
	return optionalText(value, field, maxLen)
}

func optionalText(value, field string, maxLen int) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxLen {
		return "", invalid("%s must be at most %d characters", field, maxLen)
	}
	return value, nil
}

func paging(limit, offset int32) (int, int, error) {
	if limit < 0 || limit > maxLimit {
		return 0, 0, invalid("limit must be between 1 and %d", maxLimit)
	}
	if offset < 0 {
		return 0, 0, invalid("offset must not be negative")
	}
	if limit == 0 {
		limit = defaultLimit
	}
	return int(limit), int(offset), nil
}

func money(v float64, field string) (float64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > maxMoney {
		return 0, invalid("%s must be between 0 and %d", field, maxMoney)
	}
	return round2(v), nil
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func parseDate(s, field string) (time.Time, error) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, invalid("%s must be a date in the format YYYY-MM-DD", field)
	}
	return t, nil
}

func formatDate(t time.Time) string { return t.Format(dateLayout) }

func today(loc *time.Location) time.Time {
	n := time.Now().In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func parseClock(s, field string) (string, int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return "", 0, invalid("%s must be a time in the format HH:MM (24h)", field)
	}
	return t.Format("15:04"), t.Hour()*60 + t.Minute(), nil
}

func clockMinutes(s string) int {
	_, m, _ := parseClock(s, "")
	return m
}

func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

func exists(db *gorm.DB, model any, id string) (bool, error) {
	var n int64
	if err := db.Model(model).Where("id = ?", id).Limit(1).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func mustExist(db *gorm.DB, model any, id, entity string) error {
	ok, err := exists(db, model, id)
	if err != nil {
		return dbErr(err)
	}
	if !ok {
		return notFound(entity)
	}
	return nil
}

func loadEmployee(db *gorm.DB, id string) (*models.Employee, error) {
	var e models.Employee
	err := db.Preload("Schedule").Take(&e, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, notFound("employee")
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return &e, nil
}

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func tsPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func fullName(e *models.Employee) string {
	return strings.TrimSpace(e.FirstName + " " + e.LastName)
}

func employeeSummary(e *models.Employee) *pb.EmployeeSummary {
	if e == nil || e.ID == "" {
		return nil
	}
	return &pb.EmployeeSummary{Id: e.ID, FullName: fullName(e), Position: e.Position}
}

func enumName(v int32, field string, names map[int32]string) (string, error) {
	name, ok := names[v]
	if !ok || v == 0 {
		return "", invalid("%s has an invalid value", field)
	}
	return name, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
