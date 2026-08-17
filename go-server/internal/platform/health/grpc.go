package health

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type GRPCServer struct {
	healthpb.UnimplementedHealthServer
	service *Service
}

func NewGRPCServer(service *Service) *GRPCServer {
	return &GRPCServer{service: service}
}

func RegisterGRPC(server *grpc.Server, service *Service) {
	if server == nil {
		return
	}
	healthpb.RegisterHealthServer(server, NewGRPCServer(service))
}

func ProbeGRPC(ctx context.Context, client healthpb.HealthClient, service string) (State, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return StateUnknown, err
	}
	if client == nil {
		return StateUnknown, nil
	}
	resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: strings.TrimSpace(service)})
	if err != nil {
		return StateUnknown, err
	}
	return grpcStateFromStatus(resp.GetStatus()), nil
}

func (s *GRPCServer) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.service == nil {
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVICE_UNKNOWN}, nil
	}
	if req != nil && strings.TrimSpace(req.Service) != "" && strings.TrimSpace(req.Service) != s.service.service {
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVICE_UNKNOWN}, nil
	}
	return &healthpb.HealthCheckResponse{Status: grpcServingStatus(s.service.Snapshot().State)}, nil
}

func (s *GRPCServer) Watch(req *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	resp, err := s.Check(stream.Context(), req)
	if err != nil {
		return err
	}
	return stream.Send(resp)
}

func grpcServingStatus(state State) healthpb.HealthCheckResponse_ServingStatus {
	switch state {
	case StateReady:
		return healthpb.HealthCheckResponse_SERVING
	case StateStarting, StateStopping, StateStopped, StateDegraded:
		return healthpb.HealthCheckResponse_NOT_SERVING
	default:
		return healthpb.HealthCheckResponse_SERVICE_UNKNOWN
	}
}

func grpcStateFromStatus(status healthpb.HealthCheckResponse_ServingStatus) State {
	switch status {
	case healthpb.HealthCheckResponse_SERVING:
		return StateReady
	case healthpb.HealthCheckResponse_NOT_SERVING:
		return StateDegraded
	default:
		return StateUnknown
	}
}
