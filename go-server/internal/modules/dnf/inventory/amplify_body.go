package inventory

// buildPurifyItemSuccessAck matches current NoPack sub_1D18210 after the
// common success byte: material slot/count, target slot, amplification type
// and amplification value. The client mutates both local items from this ACK.
func buildPurifyItemSuccessAck(result AmplifyMutationResult) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeInt16(result.MaterialSlotIndex)
	writer.writeInt32(clampInt32(result.MaterialRemainingCount))
	writer.writeInt16(result.TargetSlotIndex)
	writer.writeByte(result.AmplifyType)
	writer.writeUint16(result.AmplifyValue)
	return writer.bytes()
}

// Current NoPack sub_1D18210 displays its fixed failure message only when the
// framework-provided error byte is nonzero, hence status=0,error=1.
func buildPurifyItemErrorAck() []byte {
	return []byte{0, 1}
}

// buildInvestItemAmplifyOptionSuccessAck matches current NoPack
// sub_1D305A0. Action 2 alone appends the rolled amplification level.
func buildInvestItemAmplifyOptionSuccessAck(result AmplifyMutationResult) []byte {
	var writer packetWriter
	writer.writeByte(1)
	writer.writeByte(result.Action)
	writer.writeInt16(result.MaterialSlotIndex)
	writer.writeInt32(clampInt32(result.MaterialRemainingCount))
	writer.writeInt16(result.TargetSlotIndex)
	writer.writeByte(result.AmplifyType)
	writer.writeUint16(result.AmplifyValue)
	if result.Action == investAmplifyActionPureGold {
		writer.writeByte(result.AmplifyLevel)
	}
	return writer.bytes()
}

func buildInvestItemAmplifyOptionErrorAck(errorCode byte) []byte {
	return []byte{0, errorCode}
}
