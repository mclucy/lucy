package progress

type (
	setPercentMsg    float64
	incrPercentMsg   float64
	setMessageMsg    string
	setTitleMsg      string
	closeMsg         struct{}
	completeMsg      string
	bytesProgressMsg struct {
		read  int64
		total int64
	}
	appendLogMsg string
)
