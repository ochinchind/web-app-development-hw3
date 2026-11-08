package service

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/protobuf/types/known/emptypb"
	"gorm.io/gorm"

	"hr-service/core/internal/models"

	pb "hr-service/core/gen/hr/v1"
)

type DepartmentService struct {
	pb.UnimplementedDepartmentServiceServer
	db *gorm.DB
}

func NewDepartmentService(db *gorm.DB) *DepartmentService {
	return &DepartmentService{db: db}
}

func (s *DepartmentService) departments(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&models.Department{}).Select(`departments.*,
		(SELECT COUNT(*) FROM employees e
		  WHERE e.department_id = departments.id AND e.status <> 'TERMINATED') AS employee_count,
		(SELECT COALESCE(ROUND(AVG(e.salary), 2), 0) FROM employees e
		  WHERE e.department_id = departments.id AND e.status <> 'TERMINATED') AS average_salary`)
}

func (s *DepartmentService) CreateDepartment(ctx context.Context, req *pb.CreateDepartmentRequest) (*pb.Department, error) {
	d, err := validateDepartment(req.Name, req.Description, req.Location)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(&d).Error; err != nil {
		return nil, dbErr(err)
	}
	setHTTPStatus(ctx, 201)
	return toDepartmentPB(&d), nil
}

func (s *DepartmentService) ListDepartments(ctx context.Context, req *pb.ListDepartmentsRequest) (*pb.ListDepartmentsResponse, error) {
	limit, offset, err := paging(req.Limit, req.Offset)
	if err != nil {
		return nil, err
	}
	filter := func(db *gorm.DB) *gorm.DB {
		if q := strings.TrimSpace(req.Q); q != "" {
			db = db.Where("departments.name ILIKE ?", likePattern(q))
		}
		return db
	}

	var total int64
	if err := s.db.WithContext(ctx).Model(&models.Department{}).Scopes(filter).Count(&total).Error; err != nil {
		return nil, dbErr(err)
	}
	var list []models.Department
	if err := s.departments(ctx).Scopes(filter).Order("departments.name").
		Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		return nil, dbErr(err)
	}

	resp := &pb.ListDepartmentsResponse{Total: int32(total), Departments: make([]*pb.Department, 0, len(list))}
	for i := range list {
		resp.Departments = append(resp.Departments, toDepartmentPB(&list[i]))
	}
	return resp, nil
}

func (s *DepartmentService) GetDepartment(ctx context.Context, req *pb.GetDepartmentRequest) (*pb.Department, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	return s.load(ctx, req.Id)
}

func (s *DepartmentService) UpdateDepartment(ctx context.Context, req *pb.UpdateDepartmentRequest) (*pb.Department, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	d, err := validateDepartment(req.Name, req.Description, req.Location)
	if err != nil {
		return nil, err
	}
	return s.update(ctx, req.Id, map[string]any{"name": d.Name, "description": d.Description, "location": d.Location})
}

func (s *DepartmentService) PatchDepartment(ctx context.Context, req *pb.PatchDepartmentRequest) (*pb.Department, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	changes := map[string]any{}
	if req.Name != nil {
		name, err := requiredText(*req.Name, "name", 100)
		if err != nil {
			return nil, err
		}
		changes["name"] = name
	}
	if req.Description != nil {
		v, err := optionalText(*req.Description, "description", 1000)
		if err != nil {
			return nil, err
		}
		changes["description"] = v
	}
	if req.Location != nil {
		v, err := optionalText(*req.Location, "location", 200)
		if err != nil {
			return nil, err
		}
		changes["location"] = v
	}
	if len(changes) == 0 {
		return nil, invalid("provide at least one field to change: name, description, location")
	}
	return s.update(ctx, req.Id, changes)
}

func (s *DepartmentService) DeleteDepartment(ctx context.Context, req *pb.DeleteDepartmentRequest) (*emptypb.Empty, error) {
	if err := checkID(req.Id, "id"); err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	if err := mustExist(db, &models.Department{}, req.Id, "department"); err != nil {
		return nil, err
	}
	var staff int64
	if err := db.Model(&models.Employee{}).Where("department_id = ?", req.Id).Count(&staff).Error; err != nil {
		return nil, dbErr(err)
	}
	if staff > 0 {
		return nil, conflict("department still has %s; move or delete them first", plural(int(staff), "employee"))
	}
	if err := db.Delete(&models.Department{}, "id = ?", req.Id).Error; err != nil {
		return nil, dbErr(err)
	}
	setHTTPStatus(ctx, 204)
	return &emptypb.Empty{}, nil
}

func (s *DepartmentService) update(ctx context.Context, id string, changes map[string]any) (*pb.Department, error) {
	res := s.db.WithContext(ctx).Model(&models.Department{ID: id}).Updates(changes)
	if res.Error != nil {
		return nil, dbErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, notFound("department")
	}
	return s.load(ctx, id)
}

func (s *DepartmentService) load(ctx context.Context, id string) (*pb.Department, error) {
	var d models.Department
	err := s.departments(ctx).Where("departments.id = ?", id).Take(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, notFound("department")
	}
	if err != nil {
		return nil, dbErr(err)
	}
	return toDepartmentPB(&d), nil
}

func validateDepartment(name, description, location string) (models.Department, error) {
	var (
		d   models.Department
		err error
	)
	if d.Name, err = requiredText(name, "name", 100); err != nil {
		return d, err
	}
	if d.Description, err = optionalText(description, "description", 1000); err != nil {
		return d, err
	}
	if d.Location, err = optionalText(location, "location", 200); err != nil {
		return d, err
	}
	return d, nil
}

func toDepartmentPB(d *models.Department) *pb.Department {
	return &pb.Department{
		Id:            d.ID,
		Name:          d.Name,
		Description:   d.Description,
		Location:      d.Location,
		EmployeeCount: d.EmployeeCount,
		AverageSalary: d.AverageSalary,
		CreatedAt:     ts(d.CreatedAt),
		UpdatedAt:     ts(d.UpdatedAt),
	}
}
