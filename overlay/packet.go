package overlay

type PacketQueue chan *PacketBuffer

type PacketBuffer struct {
	Data   []byte
	Length int

	Queue PacketQueue
}

func NewPacketBuffer(packetSize int) *PacketBuffer {
	return &PacketBuffer{Data: make([]byte, packetSize)}
}

func AllocatePacketQueue(numPackets int, packetSize int) PacketQueue {
	queue := make(PacketQueue, numPackets)
	for i := 0; i < numPackets; i++ {
		buffer := NewPacketBuffer(packetSize)
		buffer.Queue = queue
		queue <- buffer
	}
	return queue
}
