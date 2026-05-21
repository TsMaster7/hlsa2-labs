// Code generated for hlsa2-labs/lab3-2. This file is wire-compatible
// with the output of protoc-gen-go v1.34+ but is committed manually so
// students can build the lab without installing protoc.
//
// Regenerate via `make gen-proto` if you need protoc-native output.
//
// Source: proto/lookup.proto

package lab32pb

import (
	reflect "reflect"
	sync "sync"

	proto "google.golang.org/protobuf/proto"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type LookupRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Key         string `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	PayloadSize string `protobuf:"bytes,2,opt,name=payload_size,json=payloadSize,proto3" json:"payload_size,omitempty"`
}

func (x *LookupRequest) Reset() {
	*x = LookupRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lookup_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *LookupRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*LookupRequest) ProtoMessage() {}

func (x *LookupRequest) ProtoReflect() protoreflect.Message {
	mi := &file_lookup_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use LookupRequest.ProtoReflect.Descriptor instead.
func (*LookupRequest) Descriptor() ([]byte, []int) {
	return file_lookup_proto_rawDescGZIP(), []int{0}
}

func (x *LookupRequest) GetKey() string {
	if x != nil {
		return x.Key
	}
	return ""
}

func (x *LookupRequest) GetPayloadSize() string {
	if x != nil {
		return x.PayloadSize
	}
	return ""
}

type LookupResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Key      string `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Payload  []byte `protobuf:"bytes,2,opt,name=payload,proto3" json:"payload,omitempty"`
	ServerUs int64  `protobuf:"varint,3,opt,name=server_us,json=serverUs,proto3" json:"server_us,omitempty"`
}

func (x *LookupResponse) Reset() {
	*x = LookupResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lookup_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *LookupResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*LookupResponse) ProtoMessage() {}

func (x *LookupResponse) ProtoReflect() protoreflect.Message {
	mi := &file_lookup_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use LookupResponse.ProtoReflect.Descriptor instead.
func (*LookupResponse) Descriptor() ([]byte, []int) {
	return file_lookup_proto_rawDescGZIP(), []int{1}
}

func (x *LookupResponse) GetKey() string {
	if x != nil {
		return x.Key
	}
	return ""
}

func (x *LookupResponse) GetPayload() []byte {
	if x != nil {
		return x.Payload
	}
	return nil
}

func (x *LookupResponse) GetServerUs() int64 {
	if x != nil {
		return x.ServerUs
	}
	return 0
}

var File_lookup_proto protoreflect.FileDescriptor

var (
	file_lookup_proto_rawDescOnce sync.Once
	file_lookup_proto_rawDescData []byte
)

func file_lookup_proto_rawDescGZIP() []byte {
	file_lookup_proto_rawDescOnce.Do(func() {
		// We don't actually gzip - the consumers tolerate raw bytes,
		// and the FileDescriptorProto is small enough that storage
		// savings would be a rounding error.
		file_lookup_proto_rawDescData = file_lookup_proto_rawDesc
	})
	return file_lookup_proto_rawDescData
}

var file_lookup_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_lookup_proto_goTypes = []interface{}{
	(*LookupRequest)(nil),  // 0: hlsa2.lab32.LookupRequest
	(*LookupResponse)(nil), // 1: hlsa2.lab32.LookupResponse
}
var file_lookup_proto_depIdxs = []int32{
	0, // 0: hlsa2.lab32.Lookup.Lookup:input_type -> hlsa2.lab32.LookupRequest
	1, // 1: hlsa2.lab32.Lookup.Lookup:output_type -> hlsa2.lab32.LookupResponse
	1, // [1:2] is the sub-list for method output_type
	0, // [0:1] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

var file_lookup_proto_rawDesc []byte

func init() { file_lookup_proto_init() }
func file_lookup_proto_init() {
	if File_lookup_proto != nil {
		return
	}

	// Build the FileDescriptorProto at init time. This is functionally
	// equivalent to the byte-blob protoc-gen-go normally emits; we just
	// construct it programmatically so the lab can ship without a
	// generated-bytes literal.
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("lookup.proto"),
		Package: proto.String("hlsa2.lab32"),
		Syntax:  proto.String("proto3"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/hlsa2-labs/lab3-2/proto;lab32pb"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("LookupRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("key"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("key"),
					},
					{
						Name:     proto.String("payload_size"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("payloadSize"),
					},
				},
			},
			{
				Name: proto.String("LookupResponse"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("key"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("key"),
					},
					{
						Name:     proto.String("payload"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
						JsonName: proto.String("payload"),
					},
					{
						Name:     proto.String("server_us"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("serverUs"),
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("Lookup"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("Lookup"),
						InputType:  proto.String(".hlsa2.lab32.LookupRequest"),
						OutputType: proto.String(".hlsa2.lab32.LookupResponse"),
					},
				},
			},
		},
	}

	rawDesc, err := proto.Marshal(fdp)
	if err != nil {
		panic("lookup.proto: marshal FileDescriptorProto: " + err.Error())
	}
	file_lookup_proto_rawDesc = rawDesc

	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_lookup_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_lookup_proto_goTypes,
		DependencyIndexes: file_lookup_proto_depIdxs,
		MessageInfos:      file_lookup_proto_msgTypes,
	}.Build()
	File_lookup_proto = out.File
	file_lookup_proto_goTypes = nil
	file_lookup_proto_depIdxs = nil
}

// x is a placeholder for protoimpl.X.NewPackagePath - we only need the
// reflect type to derive the Go import path.
type x struct{}
