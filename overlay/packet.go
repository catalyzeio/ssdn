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
	buffers := make([]PacketBuffer, numPackets)
	for _, buffer := range buffers {
		buffer.Data = make([]byte, packetSize)
		buffer.Queue = queue
		queue <- &buffer
	}
	return queue
}
