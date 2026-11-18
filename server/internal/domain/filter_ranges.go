package domain

// These are the visible inclusive ranges; the < and > options stay exclusive.
func CountInterval(name string) (*int64, *int64, bool) {
	low, high := int64(0), int64(0)
	switch name {
	case "", "all":
		return nil, nil, true
	case "none":
		return &low, &high, true
	case "low":
		low, high = 1, 5
	case "mid":
		low, high = 5, 20
	case "high":
		low = 21
		return &low, nil, true
	default:
		return nil, nil, false
	}
	return &low, &high, true
}
func SizeInterval(name string) (*int64, *int64, bool) {
	const mib = int64(1 << 20)
	low, high := int64(0), int64(0)
	switch name {
	case "", "all":
		return nil, nil, true
	case "lt1m":
		high = mib - 1
	case "m1to10":
		low, high = mib, 10*mib
	case "m10to50":
		low, high = 10*mib, 50*mib
	case "gt50m":
		low = 50*mib + 1
		return &low, nil, true
	default:
		return nil, nil, false
	}
	return &low, &high, true
}
