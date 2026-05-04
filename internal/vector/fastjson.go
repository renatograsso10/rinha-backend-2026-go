package vector

import "bytes"

var (
	kTransaction    = []byte(`"transaction"`)
	kCustomer       = []byte(`"customer"`)
	kMerchant       = []byte(`"merchant"`)
	kTerminal       = []byte(`"terminal"`)
	kLastNull       = []byte(`"last_transaction":null`)
	kLast           = []byte(`"last_transaction"`)
	kAmount         = []byte(`"amount":`)
	kInstallments   = []byte(`"installments":`)
	kRequestedAt    = []byte(`"requested_at":"`)
	kAvgAmount      = []byte(`"avg_amount":`)
	kTxCount24h     = []byte(`"tx_count_24h":`)
	kKnownMerchants = []byte(`"known_merchants":[`)
	kID             = []byte(`"id":"`)
	kMCC            = []byte(`"mcc":"`)
	kIsOnline       = []byte(`"is_online":`)
	kCardPresent    = []byte(`"card_present":`)
	kKmFromHome     = []byte(`"km_from_home":`)
	kTimestamp      = []byte(`"timestamp":"`)
	kKmFromCurrent  = []byte(`"km_from_current":`)
)

func VectorizeJSON(b []byte, n Normalization, mcc map[string]float32) ([Dims]float32, bool) {
	var out [Dims]float32
	tx := findFrom(b, kTransaction, 0)
	cu := findFrom(b, kCustomer, tx)
	me := findFrom(b, kMerchant, cu)
	te := findFrom(b, kTerminal, me)
	if tx < 0 || cu < 0 || me < 0 || te < 0 {
		return out, false
	}
	amount, _, ok := numberAfter(b, kAmount, tx)
	if !ok {
		return out, false
	}
	installments, _, ok := numberAfter(b, kInstallments, tx)
	if !ok {
		return out, false
	}
	req, _, ok := stringAfter(b, kRequestedAt, tx)
	if !ok {
		return out, false
	}
	reqMin, hour, dow, ok := parseTime(req)
	if !ok {
		return out, false
	}
	customerAvg, _, ok := numberAfter(b, kAvgAmount, cu)
	if !ok {
		return out, false
	}
	txCount, _, ok := numberAfter(b, kTxCount24h, cu)
	if !ok {
		return out, false
	}
	knownStart := findFrom(b, kKnownMerchants, cu)
	if knownStart < 0 {
		return out, false
	}
	knownStart += len(kKnownMerchants)
	knownEnd := findByteFrom(b, ']', knownStart)
	if knownEnd < 0 {
		return out, false
	}
	merchantID, _, ok := stringAfter(b, kID, me)
	if !ok {
		return out, false
	}
	merchantMCC, _, ok := stringAfter(b, kMCC, me)
	if !ok {
		return out, false
	}
	merchantAvg, _, ok := numberAfter(b, kAvgAmount, me)
	if !ok {
		return out, false
	}
	isOnline, _, ok := boolAfter(b, kIsOnline, te)
	if !ok {
		return out, false
	}
	cardPresent, _, ok := boolAfter(b, kCardPresent, te)
	if !ok {
		return out, false
	}
	kmHome, _, ok := numberAfter(b, kKmFromHome, te)
	if !ok {
		return out, false
	}

	out[0] = clamp32(amount / n.MaxAmount)
	out[1] = clamp32(installments / n.MaxInstallments)
	if customerAvg > 0 {
		out[2] = clamp32((amount / customerAvg) / n.AmountVsAvgRatio)
	}
	out[3] = float32(hour) / 23
	out[4] = float32(dow) / 6
	out[5], out[6] = -1, -1
	if findFrom(b, kLastNull, te) < 0 {
		la := findFrom(b, kLast, te)
		if la < 0 {
			return out, false
		}
		last, _, ok := stringAfter(b, kTimestamp, la)
		if !ok {
			return out, false
		}
		lastMin, _, _, ok := parseTime(last)
		if !ok || lastMin > reqMin {
			return out, false
		}
		kmCurrent, _, ok := numberAfter(b, kKmFromCurrent, la)
		if !ok {
			return out, false
		}
		out[5] = clamp32(float64(reqMin-lastMin) / n.MaxMinutes)
		out[6] = clamp32(kmCurrent / n.MaxKM)
	}
	out[7] = clamp32(kmHome / n.MaxKM)
	out[8] = clamp32(txCount / n.MaxTxCount24h)
	if isOnline {
		out[9] = 1
	}
	if cardPresent {
		out[10] = 1
	}
	out[11] = 1
	if containsQuoted(b[knownStart:knownEnd], merchantID) {
		out[11] = 0
	}
	if risk, ok := mcc[string(merchantMCC)]; ok {
		out[12] = risk
	} else {
		out[12] = 0.5
	}
	out[13] = clamp32(merchantAvg / n.MaxMerchantAvgAmount)
	return out, true
}

func findFrom(b, needle []byte, start int) int {
	if start < 0 || start >= len(b) {
		return -1
	}
	if i := bytes.Index(b[start:], needle); i >= 0 {
		return start + i
	}
	return -1
}

func findByteFrom(b []byte, c byte, start int) int {
	if start < 0 || start >= len(b) {
		return -1
	}
	if i := bytes.IndexByte(b[start:], c); i >= 0 {
		return start + i
	}
	return -1
}

func numberAfter(b, key []byte, start int) (float64, int, bool) {
	pos := findFrom(b, key, start)
	if pos < 0 {
		return 0, 0, false
	}
	pos += len(key)
	val := float64(0)
	div := float64(0)
	for pos < len(b) {
		c := b[pos]
		if c >= '0' && c <= '9' {
			if div == 0 {
				val = val*10 + float64(c-'0')
			} else {
				div *= 10
				val += float64(c-'0') / div
			}
			pos++
			continue
		}
		if c == '.' {
			div = 1
			pos++
			continue
		}
		break
	}
	return val, pos, true
}

func stringAfter(b, key []byte, start int) ([]byte, int, bool) {
	pos := findFrom(b, key, start)
	if pos < 0 {
		return nil, 0, false
	}
	pos += len(key)
	end := findByteFrom(b, '"', pos)
	if end < 0 {
		return nil, 0, false
	}
	return b[pos:end], end + 1, true
}

func boolAfter(b, key []byte, start int) (bool, int, bool) {
	pos := findFrom(b, key, start)
	if pos < 0 {
		return false, 0, false
	}
	pos += len(key)
	if len(b) >= pos+4 && string(b[pos:pos+4]) == "true" {
		return true, pos + 4, true
	}
	if len(b) >= pos+5 && string(b[pos:pos+5]) == "false" {
		return false, pos + 5, true
	}
	return false, 0, false
}

func containsQuoted(haystack, needle []byte) bool {
	for {
		i := bytes.Index(haystack, needle)
		if i < 0 {
			return false
		}
		before := i > 0 && haystack[i-1] == '"'
		after := i+len(needle) < len(haystack) && haystack[i+len(needle)] == '"'
		if before && after {
			return true
		}
		haystack = haystack[i+len(needle):]
	}
}

func parseTime(s []byte) (int, int, int, bool) {
	if len(s) < 20 {
		return 0, 0, 0, false
	}
	y := atoi4(s[0:4])
	mo := atoi2(s[5:7])
	d := atoi2(s[8:10])
	h := atoi2(s[11:13])
	mi := atoi2(s[14:16])
	if y <= 0 || mo <= 0 || d <= 0 || h < 0 || mi < 0 {
		return 0, 0, 0, false
	}
	return daysFromCivil(y, mo, d)*1440 + h*60 + mi, h, weekdayMonday0(y, mo, d), true
}

func atoi2(s []byte) int {
	if len(s) != 2 || s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return -1
	}
	return int(s[0]-'0')*10 + int(s[1]-'0')
}

func atoi4(s []byte) int {
	if len(s) != 4 {
		return -1
	}
	a, b, c, d := s[0], s[1], s[2], s[3]
	if a < '0' || a > '9' || b < '0' || b > '9' || c < '0' || c > '9' || d < '0' || d > '9' {
		return -1
	}
	return int(a-'0')*1000 + int(b-'0')*100 + int(c-'0')*10 + int(d-'0')
}

func weekdayMonday0(y, m, d int) int {
	t := [...]int{0, 3, 2, 5, 0, 3, 5, 1, 4, 6, 2, 4}
	if m < 3 {
		y--
	}
	sunday0 := (y + y/4 - y/100 + y/400 + t[m-1] + d) % 7
	return (sunday0 + 6) % 7
}

func daysFromCivil(y, m, d int) int {
	y -= boolToInt(m <= 2)
	era := divFloor(y, 400)
	yoe := y - era*400
	mp := m + 9
	if m > 2 {
		mp = m - 3
	}
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func divFloor(a, b int) int {
	if a >= 0 {
		return a / b
	}
	return -((-a + b - 1) / b)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
