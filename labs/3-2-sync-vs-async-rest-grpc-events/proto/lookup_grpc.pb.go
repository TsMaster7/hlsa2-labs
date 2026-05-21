// Code generated for hlsa2-labs/lab3-2. Wire-compatible with the
// output of protoc-gen-go-grpc v1.5; hand-written so the lab builds
// without protoc.
//
// Source: proto/lookup.proto

package lab32pb

import (
	context "context"

	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion7

const (
	Lookup_Lookup_FullMethodName = "/hlsa2.lab32.Lookup/Lookup"
)

// LookupClient is the client API for Lookup service.
type LookupClient interface {
	Lookup(ctx context.Context, in *LookupRequest, opts ...grpc.CallOption) (*LookupResponse, error)
}

type lookupClient struct {
	cc grpc.ClientConnInterface
}

func NewLookupClient(cc grpc.ClientConnInterface) LookupClient {
	return &lookupClient{cc}
}

func (c *lookupClient) Lookup(ctx context.Context, in *LookupRequest, opts ...grpc.CallOption) (*LookupResponse, error) {
	out := new(LookupResponse)
	err := c.cc.Invoke(ctx, Lookup_Lookup_FullMethodName, in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LookupServer is the server API for Lookup service.
// All implementations must embed UnimplementedLookupServer for forward
// compatibility.
type LookupServer interface {
	Lookup(context.Context, *LookupRequest) (*LookupResponse, error)
	mustEmbedUnimplementedLookupServer()
}

// UnimplementedLookupServer must be embedded to have forward compatible
// implementations.
type UnimplementedLookupServer struct{}

func (UnimplementedLookupServer) Lookup(context.Context, *LookupRequest) (*LookupResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Lookup not implemented")
}
func (UnimplementedLookupServer) mustEmbedUnimplementedLookupServer() {}

// UnsafeLookupServer may be embedded to opt out of forward compatibility
// for this service. Use of this interface is not recommended.
type UnsafeLookupServer interface {
	mustEmbedUnimplementedLookupServer()
}

func RegisterLookupServer(s grpc.ServiceRegistrar, srv LookupServer) {
	s.RegisterService(&Lookup_ServiceDesc, srv)
}

func _Lookup_Lookup_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(LookupRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LookupServer).Lookup(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Lookup_Lookup_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(LookupServer).Lookup(ctx, req.(*LookupRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// Lookup_ServiceDesc is the grpc.ServiceDesc for Lookup service. It's
// only intended for direct use with grpc.RegisterService, and not to be
// introspected or modified (even as a copy).
var Lookup_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "hlsa2.lab32.Lookup",
	HandlerType: (*LookupServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Lookup",
			Handler:    _Lookup_Lookup_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "lookup.proto",
}
