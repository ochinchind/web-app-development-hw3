package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"gorm.io/gorm"

	"hr-service/core/internal/models"

	pb "hr-service/core/gen/hr/v1"
)

type ScheduleService struct {
	pb.UnimplementedScheduleServiceServer
	db *gorm.DB
}

func NewScheduleService(db *gorm.DB) *ScheduleService {
	return &ScheduleService{db: db}
}

func (s *ScheduleService) schedules(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&models.WorkSchedule{}).Select(`work_schedules.*,
		(SELECT COUNT(*) FROM employees e
		  WHERE e.schedule_id = work_schedules.id AND e.status <> 'TERMINATED') AS employee_count`)
}

func (s *ScheduleService) CreateSchedule(ctx context.Context, req *pb.CreateScheduleRequest) (*pb.WorkSchedule, error) {
	w, err := validateSchedule(req.Name, req.WorkDays, req.StartTime, req.EndTime, req.LateGraceMinutes)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(&w).Error; err != nil {
		return nil, dbErr(err)
	}
	setHTTPStatus(ctx, 201)
	return toSchedulePB(&w), nil
}

func (s *ScheduleService) ListSchedules(ctx context.Context, _ *emptypb.Empty) (*pb.ListSchedulesResponse, error) {
	var list []models.WorkSchedule
	if err := s.schedules(ctx).Order("work_schedules.name").Find(&list).Error; err != nil {
		return nil, dbErr(err)
	}
	resp := &pb.ListSchedulesResponse{Schedules: make([]*pb.WorkSchedule, 0, len(list))}
	for i := range list {
		resp.Schedules = append(resp.Schedules, toSchedulePB(&list[i]))
	}
	return resp, nil
}

func (s *ScheduleService) GetSchedule(ctx context.Context, req *pb.GetScheduleRequest) (*pb.WorkSchedule, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	return s.load(ctx, req.Id)
}

func (s *ScheduleService) UpdateSchedule(ctx context.Context, req *pb.UpdateScheduleRequest) (*pb.WorkSchedule, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	w, err := validateSchedule(req.Name, req.WorkDays, req.StartTime, req.EndTime, req.LateGraceMinutes)
	if err != nil {
		return nil, err
	}
	res := s.db.WithContext(ctx).Model(&models.WorkSchedule{ID: req.Id}).Updates(map[string]any{
		"name":               w.Name,
		"work_days":          w.WorkDays,
		"start_time":         w.StartTime,
		"end_time":           w.EndTime,
		"late_grace_minutes": w.LateGraceMinutes,
	})
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, notFound("schedule")
	}
	return s.load(ctx, req.Id)
}

func (s *ScheduleService) DeleteSchedule(ctx context.Context, req *pb.DeleteScheduleRequest) (*emptypb.Empty, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	res := s.db.WithContext(ctx).Delete(&models.WorkSchedule{}, "id = ?", req.Id)
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, notFound("schedule")
	}
	setHTTPStatus(ctx, 204)
	return &emptypb.Empty{}, nil
}

func (s *ScheduleService) load(ctx context.Context, id string) (*pb.WorkSchedule, error) {
	var w models.WorkSchedule
	err := s.schedules(ctx).Where("work_schedules.id = ?", id).Take(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, notFound("schedule")
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return toSchedulePB(&w), nil
}

func validateSchedule(name string, days []int32, start, end string, grace int32) (models.WorkSchedule, error) {
	var (
		w   models.WorkSchedule
		err error
	)
	if w.Name, err = requiredText(name, "name", 100); err != nil {
		return w, err
	}
	if len(days) == 0 {
		return w, invalid("work_days must contain at least one day (1 = Monday ... 7 = Sunday)")
	}
	unique := make([]int, 0, len(days))
	for _, d := range days {
		if d < 1 || d > 7 {
			return w, invalid("work_days values must be between 1 (Monday) and 7 (Sunday)")
		}
		if !slices.Contains(unique, int(d)) {
			unique = append(unique, int(d))
		}
	}
	slices.Sort(unique)
	parts := make([]string, len(unique))
	for i, d := range unique {
		parts[i] = strconv.Itoa(d)
	}
	w.WorkDays = strings.Join(parts, ",")

	var startMin, endMin int
	if w.StartTime, startMin, err = parseClock(start, "start_time"); err != nil {
		return w, err
	}
	if w.EndTime, endMin, err = parseClock(end, "end_time"); err != nil {
		return w, err
	}
	if endMin <= startMin {
		return w, invalid("end_time must be later than start_time")
	}
	if grace < 0 || grace > 240 {
		return w, invalid("late_grace_minutes must be between 0 and 240")
	}
	w.LateGraceMinutes = grace
	return w, nil
}

func workDays(w *models.WorkSchedule) []int32 {
	var out []int32
	for _, p := range strings.Split(w.WorkDays, ",") {
		if d, err := strconv.Atoi(p); err == nil {
			out = append(out, int32(d))
		}
	}
	return out
}

func isWorkDay(w *models.WorkSchedule, date time.Time) bool {
	weekday := int32(date.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	if w == nil {
		return weekday <= 5
	}
	return slices.Contains(workDays(w), weekday)
}

func lateMinutes(w *models.WorkSchedule, date time.Time, checkIn string) int32 {
	if w == nil || !isWorkDay(w, date) {
		return 0
	}
	start := clockMinutes(w.StartTime)
	arrived := clockMinutes(checkIn)
	if arrived <= start+int(w.LateGraceMinutes) {
		return 0
	}
	return int32(arrived - start)
}

func toSchedulePB(w *models.WorkSchedule) *pb.WorkSchedule {
	days := workDays(w)
	hours := float64(len(days)*(clockMinutes(w.EndTime)-clockMinutes(w.StartTime))) / 60
	return &pb.WorkSchedule{
		Id:               w.ID,
		Name:             w.Name,
		WorkDays:         days,
		StartTime:        w.StartTime,
		EndTime:          w.EndTime,
		LateGraceMinutes: w.LateGraceMinutes,
		HoursPerWeek:     round2(hours),
		EmployeeCount:    w.EmployeeCount,
		CreatedAt:        ts(w.CreatedAt),
		UpdatedAt:        ts(w.UpdatedAt),
	}
}
