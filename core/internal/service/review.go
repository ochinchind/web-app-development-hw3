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

var reviewPeriodRe = regexp.MustCompile(`^\d{4}-Q[1-4]$`)

type PerformanceReviewService struct {
	pb.UnimplementedPerformanceReviewServiceServer
	db  *gorm.DB
	loc *time.Location
}

func NewPerformanceReviewService(db *gorm.DB, loc *time.Location) *PerformanceReviewService {
	return &PerformanceReviewService{db: db, loc: loc}
}

func (s *PerformanceReviewService) reviews(db *gorm.DB) *gorm.DB {
	return db.Model(&models.PerformanceReview{}).Preload("Employee").Preload("Reviewer")
}

func (s *PerformanceReviewService) CreateReview(ctx context.Context, req *pb.CreateReviewRequest) (*pb.PerformanceReview, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	r := models.PerformanceReview{EmployeeID: req.EmployeeId}
	var err error
	if r.ReviewerID, err = optionalID(req.ReviewerId, "reviewer_id"); err != nil {
		return nil, err
	}
	if r.Period, err = normalizeReviewPeriod(req.Period); err != nil {
		return nil, err
	}
	if r.Score, err = validateScore(req.Score); err != nil {
		return nil, err
	}
	if r.Comment, err = optionalText(req.Comment, "comment", 2000); err != nil {
		return nil, err
	}
	if r.Goals, err = optionalText(req.Goals, "goals", 2000); err != nil {
		return nil, err
	}
	if r.ReviewDate, err = s.reviewDate(req.ReviewDate); err != nil {
		return nil, err
	}

	db := s.db.WithContext(ctx)
	employee, err := loadEmployee(db, req.EmployeeId)
	if err != nil {
		return nil, err
	}
	if employee.Status == models.StatusTerminated {
		return nil, conflict("cannot review a terminated employee")
	}
	if err := checkReviewer(db, r.ReviewerID, r.EmployeeID); err != nil {
		return nil, err
	}
	if err := db.Omit("Employee", "Reviewer").Create(&r).Error; err != nil {
		return nil, dbErr(err)
	}
	setHTTPStatus(ctx, 201)
	return s.load(ctx, r.ID)
}

func (s *PerformanceReviewService) ListEmployeeReviews(ctx context.Context, req *pb.ListEmployeeReviewsRequest) (*pb.ListReviewsResponse, error) {
	if err := checkID(req.EmployeeId, "employee_id"); err != nil {
		return nil, err
	}
	limit, offset, err := paging(req.Limit, req.Offset)
	if err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	if err := mustExist(db, &models.Employee{}, req.EmployeeId, "employee"); err != nil {
		return nil, err
	}

	var stats struct {
		Total   int64
		Average float64
	}
	if err := db.Model(&models.PerformanceReview{}).
		Select("COUNT(*) AS total, COALESCE(ROUND(AVG(score), 2), 0) AS average").
		Where("employee_id = ?", req.EmployeeId).Scan(&stats).Error; err != nil {
		return nil, dbErr(err)
	}
	var list []models.PerformanceReview
	if err := s.reviews(db).Where("employee_id = ?", req.EmployeeId).
		Order("period DESC, created_at DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		return nil, dbErr(err)
	}

	resp := &pb.ListReviewsResponse{
		Total:        int32(stats.Total),
		AverageScore: stats.Average,
		Reviews:      make([]*pb.PerformanceReview, 0, len(list)),
	}
	for i := range list {
		resp.Reviews = append(resp.Reviews, toReviewPB(&list[i]))
	}
	return resp, nil
}

func (s *PerformanceReviewService) GetReview(ctx context.Context, req *pb.GetReviewRequest) (*pb.PerformanceReview, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	return s.load(ctx, req.Id)
}

func (s *PerformanceReviewService) PatchReview(ctx context.Context, req *pb.PatchReviewRequest) (*pb.PerformanceReview, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	changes := map[string]any{}
	var (
		reviewer *string
		err      error
	)
	if req.ReviewerId != nil {
		if reviewer, err = optionalID(*req.ReviewerId, "reviewer_id"); err != nil {
			return nil, err
		}
		changes["reviewer_id"] = reviewer
	}
	if req.Period != nil {
		if changes["period"], err = normalizeReviewPeriod(*req.Period); err != nil {
			return nil, err
		}
	}
	if req.Score != nil {
		if changes["score"], err = validateScore(*req.Score); err != nil {
			return nil, err
		}
	}
	if req.Comment != nil {
		if changes["comment"], err = optionalText(*req.Comment, "comment", 2000); err != nil {
			return nil, err
		}
	}
	if req.Goals != nil {
		if changes["goals"], err = optionalText(*req.Goals, "goals", 2000); err != nil {
			return nil, err
		}
	}
	if req.ReviewDate != nil {
		if changes["review_date"], err = s.reviewDate(*req.ReviewDate); err != nil {
			return nil, err
		}
	}
	if len(changes) == 0 {
		return nil, invalid("provide at least one field to change: reviewer_id, period, score, comment, goals, review_date")
	}

	db := s.db.WithContext(ctx)
	var current models.PerformanceReview
	if err := db.Take(&current, "id = ?", req.Id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound("performance review")
		}
		return nil, dbErr(err)
	}
	if err := checkReviewer(db, reviewer, current.EmployeeID); err != nil {
		return nil, err
	}
	if err := db.Model(&current).Updates(changes).Error; err != nil {
		return nil, dbErr(err)
	}
	return s.load(ctx, req.Id)
}

func (s *PerformanceReviewService) DeleteReview(ctx context.Context, req *pb.DeleteReviewRequest) (*emptypb.Empty, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	res := s.db.WithContext(ctx).Delete(&models.PerformanceReview{}, "id = ?", req.Id)
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, notFound("performance review")
	}
	setHTTPStatus(ctx, 204)
	return &emptypb.Empty{}, nil
}

func (s *PerformanceReviewService) load(ctx context.Context, id string) (*pb.PerformanceReview, error) {
	var r models.PerformanceReview
	err := s.reviews(s.db.WithContext(ctx)).Take(&r, "performance_reviews.id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, notFound("performance review")
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return toReviewPB(&r), nil
}

// reviewDate defaults to today and must not be in the future.
func (s *PerformanceReviewService) reviewDate(v string) (time.Time, error) {
	now := today(s.loc)
	if strings.TrimSpace(v) == "" {
		return now, nil
	}
	d, err := parseDate(v, "review_date")
	if err != nil {
		return d, err
	}
	if d.After(now) {
		return d, invalid("review_date must not be in the future")
	}
	return d, nil
}

func checkReviewer(db *gorm.DB, reviewerID *string, employeeID string) error {
	if reviewerID == nil {
		return nil
	}
	if *reviewerID == employeeID {
		return invalid("an employee cannot review themselves")
	}
	ok, err := exists(db, &models.Employee{}, *reviewerID)
	if err != nil {
		return dbErr(err)
	}
	if !ok {
		return invalid("reviewer_id does not reference an existing employee")
	}
	return nil
}

func normalizeReviewPeriod(v string) (string, error) {
	v = strings.ToUpper(strings.TrimSpace(v))
	if !reviewPeriodRe.MatchString(v) {
		return "", invalid("period must look like 2026-Q3 (year and quarter)")
	}
	return v, nil
}

func validateScore(score int32) (int32, error) {
	if score < 1 || score > 5 {
		return 0, invalid("score must be between 1 and 5")
	}
	return score, nil
}

func toReviewPB(r *models.PerformanceReview) *pb.PerformanceReview {
	return &pb.PerformanceReview{
		Id:         r.ID,
		EmployeeId: r.EmployeeID,
		Employee:   employeeSummary(&r.Employee),
		ReviewerId: derefStr(r.ReviewerID),
		Reviewer:   employeeSummary(r.Reviewer),
		Period:     r.Period,
		Score:      r.Score,
		Comment:    r.Comment,
		Goals:      r.Goals,
		ReviewDate: formatDate(r.ReviewDate),
		CreatedAt:  ts(r.CreatedAt),
		UpdatedAt:  ts(r.UpdatedAt),
	}
}
