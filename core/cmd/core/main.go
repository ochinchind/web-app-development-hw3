package main

import (
	"context"
	"log"
	"net"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"hr-service/core/internal/config"
	"hr-service/core/internal/db"
	"hr-service/core/internal/service"

	pb "hr-service/core/gen/hr/v1"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	gdb, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Migrate(gdb); err != nil {
		log.Fatal(err)
	}

	server := grpc.NewServer(grpc.ChainUnaryInterceptor(logAndRecover, service.ConflictStatusInterceptor))

	pb.RegisterDepartmentServiceServer(server, service.NewDepartmentService(gdb))
	pb.RegisterEmployeeServiceServer(server, service.NewEmployeeService(gdb, cfg.Location))
	pb.RegisterScheduleServiceServer(server, service.NewScheduleService(gdb))
	pb.RegisterPerformanceReviewServiceServer(server, service.NewPerformanceReviewService(gdb, cfg.Location))
	pb.RegisterAttendanceServiceServer(server, service.NewAttendanceService(gdb, cfg.Location))
	pb.RegisterPayrollServiceServer(server, service.NewPayrollService(gdb, cfg.Location))
	pb.RegisterLeaveServiceServer(server, service.NewLeaveService(gdb))
	healthpb.RegisterHealthServer(server, health.NewServer())
	reflection.Register(server)

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		<-ctx.Done()
		log.Print("shutting down")
		server.GracefulStop()
	}()

	log.Printf("core gRPC server listening on :%s", cfg.GRPCPort)
	if err := server.Serve(lis); err != nil {
		log.Fatal(err)
	}
}

func logAndRecover(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (resp any, err error) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in %s: %v", info.FullMethod, r)
			err = status.Error(codes.Internal, "internal server error")
		}
		log.Printf("%s %s %s", info.FullMethod, status.Code(err), time.Since(start).Round(time.Microsecond))
	}()
	return next(ctx, req)
}
