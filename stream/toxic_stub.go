package stream

type ToxicStub struct {
	Input     <-chan *StreamChunk
	Output    chan<- *StreamChunk
	Interrupt chan struct{}
	running   chan struct{}
	closed    chan struct{}
}

func NewToxicStub(input <-chan *StreamChunk, output chan<- *StreamChunk) *ToxicStub {
	return &ToxicStub{
		Interrupt: make(chan struct{}),
		closed:    make(chan struct{}),
		Input:     input,
		Output:    output,
	}
}

// Begin running a toxic on this stub, can be interrupted.
//  Does not use the Toxic interface to avoid import loops
func (s *ToxicStub) Run(toxic interface {
	Pipe(*ToxicStub)
}) {
	s.running = make(chan struct{})
	defer close(s.running)
	toxic.Pipe(s)
}

// Interrupt the flow of data so that the toxic controlling the stub can be replaced.
// Returns true if the stream was successfully interrupted.
func (s *ToxicStub) InterruptToxic() bool {
	select {
	case <-s.closed:
		return false
	case s.Interrupt <- struct{}{}:
		<-s.running // Wait for the running toxic to exit
		return true
	}
}

func (s *ToxicStub) Close() {
	close(s.closed)
	close(s.Output)
}
