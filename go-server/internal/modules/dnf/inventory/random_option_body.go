package inventory

func buildRandomOptionStatusAck(success bool) []byte {
	if success {
		return []byte{1}
	}
	return []byte{0}
}
