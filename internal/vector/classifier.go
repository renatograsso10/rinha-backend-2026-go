package vector

const LinearThreshold = float32(-2.6574135)

func LinearApproved(v [Dims]float32) bool {
	return LinearRisk(v) < LinearThreshold
}

func LinearRisk(v [Dims]float32) float32 {
	amount := v[0]
	installments := v[1]
	amountVsAvg := v[2]
	mins := v[5]
	kmLast := v[6]
	home := v[7]
	tx24 := v[8]
	online := v[9]
	cardPresent := v[10]
	unknown := v[11]
	risk := v[12]
	merchantAvg := v[13]
	lastNull := float32(0)
	if mins < 0 {
		lastNull = 1
	}
	return -5.355121 +
		1.333199*amount +
		3.650420*installments +
		2.526394*amountVsAvg +
		2.690394*home +
		3.409154*tx24 +
		1.121705*unknown +
		0.097614*online -
		0.105904*cardPresent +
		1.801797*risk -
		31.521272*merchantAvg +
		0.290156*lastNull +
		0.484335*kmLast -
		0.290156*mins -
		0.190208*amount*home -
		0.399466*amount*unknown -
		0.242746*home*unknown +
		0.979421*amountVsAvg*tx24 +
		0.097614*online*(1-cardPresent) -
		0.193848*risk*unknown +
		0.157298*amount*risk +
		0.826392*home*risk +
		0.123126*tx24*unknown
}
