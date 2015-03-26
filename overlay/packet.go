package overlay

type PacketBuffer struct {
	Data   []byte
	Length int
}

func NewPacketBuffer(n int) *PacketBuffer {
	return &PacketBuffer{Data: make([]byte, n)}
}

func NewPacketBuffers(m int, n int) []PacketBuffer {
	buffers := make([]PacketBuffer, m)
	for i := 0; i < m; i++ {
		buffers[i].Data = make([]byte, n)
	}
	return buffers
}
