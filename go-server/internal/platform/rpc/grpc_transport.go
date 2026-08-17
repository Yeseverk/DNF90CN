package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"

	"longheng.io/server/internal/platform/runtimeguard"
	"longheng.io/server/pkg/contracts"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// ErrTransportClosed 表示 gRPC transport 已关闭或未正确装配。
var ErrTransportClosed = errors.New("rpc transport is closed")

// ErrTransportCredReq 用于 errors.Is 判断生产强制 TLS/mTLS 时仍缺少 gRPC 传输凭据的配置错误。
var ErrTransportCredReq = errors.New("rpc transport credentials are required")

// GRPCTransportOptions 是 gRPC transport 的监听、节点表和安全凭据配置。
type GRPCTransportOptions struct {
	ListenAddress            string
	Environment              string
	Peers                    map[string]string
	ServerCredentials        credentials.TransportCredentials
	DialCredentials          credentials.TransportCredentials
	RequireTransportSecurity bool
	ServerOptions            []grpc.ServerOption
	DialOptions              []grpc.DialOption
}

// GRPCTransport 使用 gRPC 在不同节点之间传递 RPC 请求和响应。
type GRPCTransport struct {
	mu               sync.RWMutex
	listener         net.Listener
	server           *grpc.Server
	listenAddress    string
	peers            map[string]string
	requestHandlers  map[string]RequestHandler
	responseHandlers map[string]ResponseHandler
	conns            map[string]*grpc.ClientConn
	dialCredentials  credentials.TransportCredentials
	dialOptions      []grpc.DialOption
	closed           bool
}

// NewGRPCTransport 创建并启动一个 gRPC transport。
func NewGRPCTransport(options GRPCTransportOptions) (*GRPCTransport, error) {
	address := strings.TrimSpace(options.ListenAddress)
	if address == "" {
		address = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	needSecureTransport := options.RequireTransportSecurity || runtimeguard.IsProductionEnvironment(options.Environment)
	if needSecureTransport && (options.ServerCredentials == nil || options.DialCredentials == nil) {
		_ = listener.Close()
		return nil, ErrTransportCredReq
	}
	serverOptions := []grpc.ServerOption{grpc.ForceServerCodec(rpcJSONCodec{})}
	if options.ServerCredentials != nil {
		serverOptions = append(serverOptions, grpc.Creds(options.ServerCredentials.Clone()))
	}
	serverOptions = append(serverOptions, options.ServerOptions...)
	transport := &GRPCTransport{
		listener:         listener,
		server:           grpc.NewServer(serverOptions...),
		listenAddress:    listener.Addr().String(),
		peers:            cloneStringMap(options.Peers),
		requestHandlers:  make(map[string]RequestHandler),
		responseHandlers: make(map[string]ResponseHandler),
		conns:            make(map[string]*grpc.ClientConn),
		dialCredentials:  cloneTransportCreds(options.DialCredentials),
		dialOptions:      append([]grpc.DialOption(nil), options.DialOptions...),
	}
	regGRPCWire(transport.server, transport)
	go func() {
		_ = transport.server.Serve(listener)
	}()
	return transport, nil
}

// ListenAddress 返回实际监听地址，端口为 0 时可用于发现系统分配端口。
func (t *GRPCTransport) ListenAddress() string {
	if t == nil {
		return ""
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.listenAddress
}

// SetPeer 设置或删除目标节点到 gRPC 地址的映射。
func (t *GRPCTransport) SetPeer(nodeID, address string) {
	if t == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	address = strings.TrimSpace(address)
	if nodeID == "" {
		return
	}
	t.mu.Lock()
	if t.peers == nil {
		t.peers = make(map[string]string)
	}
	if address == "" {
		delete(t.peers, nodeID)
	} else {
		t.peers[nodeID] = address
	}
	t.mu.Unlock()
}

// SubscribeRequests 注册当前节点的请求处理函数。
func (t *GRPCTransport) SubscribeRequests(_ context.Context, nodeID string, handler RequestHandler) (TransportSubscription, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, ErrInvalidNodeID
	}
	if handler == nil {
		return nil, ErrInvalidRoute
	}
	if t == nil {
		return nil, ErrTransportClosed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTransportClosed
	}
	t.requestHandlers[nodeID] = handler
	return transportSub(func() {
		t.mu.Lock()
		delete(t.requestHandlers, nodeID)
		t.mu.Unlock()
	}), nil
}

// SubscribeResponses 注册当前节点的响应处理函数。
func (t *GRPCTransport) SubscribeResponses(_ context.Context, nodeID string, handler ResponseHandler) (TransportSubscription, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, ErrInvalidNodeID
	}
	if handler == nil {
		return nil, ErrInvalidRoute
	}
	if t == nil {
		return nil, ErrTransportClosed
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTransportClosed
	}
	t.responseHandlers[nodeID] = handler
	return transportSub(func() {
		t.mu.Lock()
		delete(t.responseHandlers, nodeID)
		t.mu.Unlock()
	}), nil
}

// PublishRequest 通过 gRPC 把请求发送到目标节点。
func (t *GRPCTransport) PublishRequest(ctx context.Context, targetNodeID string, request contracts.RPCRequest) error {
	request.TargetNodeID = strings.TrimSpace(targetNodeID)
	return t.invoke(ctx, request.TargetNodeID, "/longheng.rpc.Transport/Request", &request, &grpcAck{})
}

// PublishResponse 通过 gRPC 把响应发送到目标节点。
func (t *GRPCTransport) PublishResponse(ctx context.Context, targetNodeID string, response contracts.RPCResponse) error {
	response.TargetNodeID = strings.TrimSpace(targetNodeID)
	return t.invoke(ctx, response.TargetNodeID, "/longheng.rpc.Transport/Response", &response, &grpcAck{})
}

// Close 停止 gRPC server 并关闭所有已缓存连接。
func (t *GRPCTransport) Close() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	server := t.server
	conns := t.conns
	t.conns = nil
	t.requestHandlers = nil
	t.responseHandlers = nil
	t.mu.Unlock()
	if server != nil {
		server.Stop()
	}
	var firstErr error
	for _, conn := range conns {
		if conn == nil {
			continue
		}
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (t *GRPCTransport) request(ctx context.Context, request *contracts.RPCRequest) (*grpcAck, error) {
	if request == nil {
		return nil, ErrInvalidRequestID
	}
	t.mu.RLock()
	handler := t.requestHandlers[strings.TrimSpace(request.TargetNodeID)]
	t.mu.RUnlock()
	if handler == nil {
		return nil, ErrNoTargetNodes
	}
	return &grpcAck{}, handler(ctx, *request)
}

func (t *GRPCTransport) response(ctx context.Context, response *contracts.RPCResponse) (*grpcAck, error) {
	if response == nil {
		return nil, ErrInvalidRequestID
	}
	t.mu.RLock()
	handler := t.responseHandlers[strings.TrimSpace(response.TargetNodeID)]
	t.mu.RUnlock()
	if handler == nil {
		return nil, ErrNoTargetNodes
	}
	return &grpcAck{}, handler(ctx, *response)
}

func (t *GRPCTransport) invoke(ctx context.Context, nodeID, method string, in any, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(nodeID) == "" {
		return ErrInvalidTarget
	}
	conn, err := t.clientConn(nodeID)
	if err != nil {
		return err
	}
	return conn.Invoke(ctx, method, in, out, grpc.ForceCodec(rpcJSONCodec{}))
}

func (t *GRPCTransport) clientConn(nodeID string) (*grpc.ClientConn, error) {
	if t == nil {
		return nil, ErrTransportClosed
	}
	t.mu.RLock()
	if t.closed {
		t.mu.RUnlock()
		return nil, ErrTransportClosed
	}
	address := strings.TrimSpace(t.peers[strings.TrimSpace(nodeID)])
	if address == "" {
		t.mu.RUnlock()
		return nil, ErrNoTargetNodes
	}
	if conn := t.conns[address]; conn != nil {
		t.mu.RUnlock()
		return conn, nil
	}
	t.mu.RUnlock()

	options := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rpcJSONCodec{})),
	}
	if t.dialCredentials != nil {
		options = append(options, grpc.WithTransportCredentials(t.dialCredentials.Clone()))
	} else {
		options = append(options, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	t.mu.RLock()
	options = append(options, t.dialOptions...)
	t.mu.RUnlock()
	conn, err := grpc.NewClient(grpcTarget(address), options...)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		_ = conn.Close()
		return nil, ErrTransportClosed
	}
	if existing := t.conns[address]; existing != nil {
		t.mu.Unlock()
		_ = conn.Close()
		return existing, nil
	}
	t.conns[address] = conn
	t.mu.Unlock()
	return conn, nil
}

func cloneTransportCreds(creds credentials.TransportCredentials) credentials.TransportCredentials {
	if creds == nil {
		return nil
	}
	return creds.Clone()
}

type transportSub func()

// Close 注销 transport 订阅。
func (s transportSub) Close() error {
	if s != nil {
		s()
	}
	return nil
}

type rpcJSONCodec struct{}

// Name 返回 gRPC codec 名称。
func (rpcJSONCodec) Name() string {
	return "json"
}

// Marshal 把 RPC 结构编码为 JSON 字节。
func (rpcJSONCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal 把 JSON 字节解码为 RPC 结构。
func (rpcJSONCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

type grpcAck struct{}

type grpcTransportService interface {
	request(context.Context, *contracts.RPCRequest) (*grpcAck, error)
	response(context.Context, *contracts.RPCResponse) (*grpcAck, error)
}

var grpcServiceDesc = grpc.ServiceDesc{
	ServiceName: "longheng.rpc.Transport",
	HandlerType: (*grpcTransportService)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Request", Handler: grpcRequestHandler},
		{MethodName: "Response", Handler: grpcResponseHandler},
	},
}

func grpcRequestHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(contracts.RPCRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(grpcTransportService).request(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/longheng.rpc.Transport/Request"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(grpcTransportService).request(ctx, req.(*contracts.RPCRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func grpcResponseHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(contracts.RPCResponse)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(grpcTransportService).response(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/longheng.rpc.Transport/Response"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(grpcTransportService).response(ctx, req.(*contracts.RPCResponse))
	}
	return interceptor(ctx, in, info, handler)
}

func regGRPCWire(server *grpc.Server, transport *GRPCTransport) {
	server.RegisterService(&grpcServiceDesc, transport)
}

func grpcTarget(address string) string {
	address = strings.TrimSpace(address)
	if strings.Contains(address, "://") {
		return address
	}
	return "passthrough:///" + address
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
