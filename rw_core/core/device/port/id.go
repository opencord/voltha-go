package port

// LogicalPorts are uniquely identified by a Device ID & PortNo
type ID struct {
	DeviceID string
	PortNo   uint32
}
