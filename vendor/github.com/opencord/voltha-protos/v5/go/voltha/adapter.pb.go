// Code generated by protoc-gen-go. DO NOT EDIT.
// source: voltha_protos/adapter.proto

package voltha

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	any "github.com/golang/protobuf/ptypes/any"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

type AdapterConfig struct {
	// Custom (vendor-specific) configuration attributes
	AdditionalConfig     *any.Any `protobuf:"bytes,64,opt,name=additional_config,json=additionalConfig,proto3" json:"additional_config,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *AdapterConfig) Reset()         { *m = AdapterConfig{} }
func (m *AdapterConfig) String() string { return proto.CompactTextString(m) }
func (*AdapterConfig) ProtoMessage()    {}
func (*AdapterConfig) Descriptor() ([]byte, []int) {
	return fileDescriptor_7e998ce153307274, []int{0}
}

func (m *AdapterConfig) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_AdapterConfig.Unmarshal(m, b)
}
func (m *AdapterConfig) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_AdapterConfig.Marshal(b, m, deterministic)
}
func (m *AdapterConfig) XXX_Merge(src proto.Message) {
	xxx_messageInfo_AdapterConfig.Merge(m, src)
}
func (m *AdapterConfig) XXX_Size() int {
	return xxx_messageInfo_AdapterConfig.Size(m)
}
func (m *AdapterConfig) XXX_DiscardUnknown() {
	xxx_messageInfo_AdapterConfig.DiscardUnknown(m)
}

var xxx_messageInfo_AdapterConfig proto.InternalMessageInfo

func (m *AdapterConfig) GetAdditionalConfig() *any.Any {
	if m != nil {
		return m.AdditionalConfig
	}
	return nil
}

// Adapter (software plugin)
type Adapter struct {
	// the adapter ID has to be unique,
	// it will be generated as Type + CurrentReplica
	Id      string `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Vendor  string `protobuf:"bytes,2,opt,name=vendor,proto3" json:"vendor,omitempty"`
	Version string `protobuf:"bytes,3,opt,name=version,proto3" json:"version,omitempty"`
	// Adapter configuration
	Config *AdapterConfig `protobuf:"bytes,16,opt,name=config,proto3" json:"config,omitempty"`
	// Custom descriptors and custom configuration
	AdditionalDescription *any.Any `protobuf:"bytes,64,opt,name=additional_description,json=additionalDescription,proto3" json:"additional_description,omitempty"`
	LogicalDeviceIds      []string `protobuf:"bytes,4,rep,name=logical_device_ids,json=logicalDeviceIds,proto3" json:"logical_device_ids,omitempty"`
	// timestamp when the adapter last sent a message to the core
	LastCommunication int64  `protobuf:"varint,5,opt,name=last_communication,json=lastCommunication,proto3" json:"last_communication,omitempty"`
	CurrentReplica    int32  `protobuf:"varint,6,opt,name=currentReplica,proto3" json:"currentReplica,omitempty"`
	TotalReplicas     int32  `protobuf:"varint,7,opt,name=totalReplicas,proto3" json:"totalReplicas,omitempty"`
	Endpoint          string `protobuf:"bytes,8,opt,name=endpoint,proto3" json:"endpoint,omitempty"`
	// all replicas of the same adapter will have the same type
	// it is used to associate a device to an adapter
	Type                 string   `protobuf:"bytes,9,opt,name=type,proto3" json:"type,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *Adapter) Reset()         { *m = Adapter{} }
func (m *Adapter) String() string { return proto.CompactTextString(m) }
func (*Adapter) ProtoMessage()    {}
func (*Adapter) Descriptor() ([]byte, []int) {
	return fileDescriptor_7e998ce153307274, []int{1}
}

func (m *Adapter) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Adapter.Unmarshal(m, b)
}
func (m *Adapter) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Adapter.Marshal(b, m, deterministic)
}
func (m *Adapter) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Adapter.Merge(m, src)
}
func (m *Adapter) XXX_Size() int {
	return xxx_messageInfo_Adapter.Size(m)
}
func (m *Adapter) XXX_DiscardUnknown() {
	xxx_messageInfo_Adapter.DiscardUnknown(m)
}

var xxx_messageInfo_Adapter proto.InternalMessageInfo

func (m *Adapter) GetId() string {
	if m != nil {
		return m.Id
	}
	return ""
}

func (m *Adapter) GetVendor() string {
	if m != nil {
		return m.Vendor
	}
	return ""
}

func (m *Adapter) GetVersion() string {
	if m != nil {
		return m.Version
	}
	return ""
}

func (m *Adapter) GetConfig() *AdapterConfig {
	if m != nil {
		return m.Config
	}
	return nil
}

func (m *Adapter) GetAdditionalDescription() *any.Any {
	if m != nil {
		return m.AdditionalDescription
	}
	return nil
}

func (m *Adapter) GetLogicalDeviceIds() []string {
	if m != nil {
		return m.LogicalDeviceIds
	}
	return nil
}

func (m *Adapter) GetLastCommunication() int64 {
	if m != nil {
		return m.LastCommunication
	}
	return 0
}

func (m *Adapter) GetCurrentReplica() int32 {
	if m != nil {
		return m.CurrentReplica
	}
	return 0
}

func (m *Adapter) GetTotalReplicas() int32 {
	if m != nil {
		return m.TotalReplicas
	}
	return 0
}

func (m *Adapter) GetEndpoint() string {
	if m != nil {
		return m.Endpoint
	}
	return ""
}

func (m *Adapter) GetType() string {
	if m != nil {
		return m.Type
	}
	return ""
}

type Adapters struct {
	Items                []*Adapter `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
	XXX_NoUnkeyedLiteral struct{}   `json:"-"`
	XXX_unrecognized     []byte     `json:"-"`
	XXX_sizecache        int32      `json:"-"`
}

func (m *Adapters) Reset()         { *m = Adapters{} }
func (m *Adapters) String() string { return proto.CompactTextString(m) }
func (*Adapters) ProtoMessage()    {}
func (*Adapters) Descriptor() ([]byte, []int) {
	return fileDescriptor_7e998ce153307274, []int{2}
}

func (m *Adapters) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Adapters.Unmarshal(m, b)
}
func (m *Adapters) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Adapters.Marshal(b, m, deterministic)
}
func (m *Adapters) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Adapters.Merge(m, src)
}
func (m *Adapters) XXX_Size() int {
	return xxx_messageInfo_Adapters.Size(m)
}
func (m *Adapters) XXX_DiscardUnknown() {
	xxx_messageInfo_Adapters.DiscardUnknown(m)
}

var xxx_messageInfo_Adapters proto.InternalMessageInfo

func (m *Adapters) GetItems() []*Adapter {
	if m != nil {
		return m.Items
	}
	return nil
}

func init() {
	proto.RegisterType((*AdapterConfig)(nil), "adapter.AdapterConfig")
	proto.RegisterType((*Adapter)(nil), "adapter.Adapter")
	proto.RegisterType((*Adapters)(nil), "adapter.Adapters")
}

func init() { proto.RegisterFile("voltha_protos/adapter.proto", fileDescriptor_7e998ce153307274) }

var fileDescriptor_7e998ce153307274 = []byte{
	// 413 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x7c, 0x52, 0x4f, 0x6b, 0xdb, 0x30,
	0x14, 0xc7, 0x49, 0xf3, 0xef, 0x95, 0x94, 0x54, 0x6c, 0x41, 0x6b, 0x2f, 0x26, 0x8c, 0xe2, 0xc3,
	0x2a, 0x43, 0xc6, 0xee, 0x4b, 0xdb, 0xcb, 0xd8, 0x4d, 0x87, 0x1d, 0xc6, 0x20, 0x28, 0x92, 0xea,
	0x0a, 0x1c, 0x3d, 0x23, 0x29, 0x86, 0x7c, 0x9e, 0x7d, 0xd1, 0x51, 0x59, 0x5e, 0xda, 0x1e, 0x7a,
	0xf3, 0xef, 0xaf, 0xdf, 0x7b, 0x08, 0xae, 0x5b, 0xac, 0xc3, 0x93, 0xd8, 0x36, 0x0e, 0x03, 0xfa,
	0x52, 0x28, 0xd1, 0x04, 0xed, 0x58, 0x84, 0x64, 0x92, 0xe0, 0xd5, 0xa7, 0x0a, 0xb1, 0xaa, 0x75,
	0x19, 0xe9, 0xdd, 0xe1, 0xb1, 0x14, 0xf6, 0xd8, 0x79, 0x56, 0x1c, 0xe6, 0x9b, 0xce, 0x75, 0x8f,
	0xf6, 0xd1, 0x54, 0x64, 0x03, 0x97, 0x42, 0x29, 0x13, 0x0c, 0x5a, 0x51, 0x6f, 0x65, 0x24, 0xe9,
	0xf7, 0x3c, 0x2b, 0xce, 0xd7, 0x1f, 0x58, 0xd7, 0xc3, 0xfa, 0x1e, 0xb6, 0xb1, 0x47, 0xbe, 0x38,
	0xd9, 0xbb, 0x8a, 0xd5, 0xdf, 0x21, 0x4c, 0x52, 0x29, 0xb9, 0x80, 0x81, 0x51, 0x34, 0xcb, 0xb3,
	0x62, 0xc6, 0x07, 0x46, 0x91, 0x25, 0x8c, 0x5b, 0x6d, 0x15, 0x3a, 0x3a, 0x88, 0x5c, 0x42, 0x84,
	0xc2, 0xa4, 0xd5, 0xce, 0x1b, 0xb4, 0x74, 0x18, 0x85, 0x1e, 0x12, 0x06, 0xe3, 0x34, 0xc5, 0x22,
	0x4e, 0xb1, 0x64, 0xfd, 0x96, 0xaf, 0x06, 0xe7, 0xc9, 0x45, 0x7e, 0xc2, 0xf2, 0xc5, 0x02, 0x4a,
	0x7b, 0xe9, 0x4c, 0xf3, 0x8c, 0xde, 0xdd, 0xe2, 0xe3, 0x29, 0xf3, 0x70, 0x8a, 0x90, 0x2f, 0x40,
	0x6a, 0xac, 0x8c, 0x8c, 0x4d, 0xad, 0x91, 0x7a, 0x6b, 0x94, 0xa7, 0x67, 0xf9, 0xb0, 0x98, 0xf1,
	0x45, 0x52, 0x1e, 0xa2, 0xf0, 0x43, 0x79, 0x72, 0x0b, 0xa4, 0x16, 0x3e, 0x6c, 0x25, 0xee, 0xf7,
	0x07, 0x6b, 0xa4, 0x88, 0xbf, 0x1d, 0xe5, 0x59, 0x31, 0xe4, 0x97, 0xcf, 0xca, 0xfd, 0x4b, 0x81,
	0xdc, 0xc0, 0x85, 0x3c, 0x38, 0xa7, 0x6d, 0xe0, 0xba, 0xa9, 0x8d, 0x14, 0x74, 0x9c, 0x67, 0xc5,
	0x88, 0xbf, 0x61, 0xc9, 0x67, 0x98, 0x07, 0x0c, 0xa2, 0x4e, 0xd8, 0xd3, 0x49, 0xb4, 0xbd, 0x26,
	0xc9, 0x15, 0x4c, 0xb5, 0x55, 0x0d, 0x1a, 0x1b, 0xe8, 0x34, 0x9e, 0xf0, 0x3f, 0x26, 0x04, 0xce,
	0xc2, 0xb1, 0xd1, 0x74, 0x16, 0xf9, 0xf8, 0xbd, 0x5a, 0xc3, 0x34, 0x1d, 0xd0, 0x93, 0x1b, 0x18,
	0x99, 0xa0, 0xf7, 0x9e, 0x66, 0xf9, 0xb0, 0x38, 0x5f, 0x2f, 0xde, 0x9e, 0x98, 0x77, 0xf2, 0xdd,
	0x1f, 0xb8, 0x46, 0x57, 0x31, 0x6c, 0xb4, 0x95, 0xe8, 0x14, 0xeb, 0x5e, 0x5f, 0xef, 0xbe, 0x9b,
	0xff, 0x8a, 0x38, 0x85, 0x7e, 0xb3, 0xca, 0x84, 0xa7, 0xc3, 0x8e, 0x49, 0xdc, 0x97, 0x7d, 0xa4,
	0xec, 0x22, 0xb7, 0xe9, 0xc1, 0xb6, 0xdf, 0xca, 0x0a, 0x13, 0xb7, 0x1b, 0x47, 0xf2, 0xeb, 0xbf,
	0x00, 0x00, 0x00, 0xff, 0xff, 0x41, 0x02, 0xed, 0xf9, 0xd5, 0x02, 0x00, 0x00,
}