package util

// CheckError ...
func CheckError(err error) {
	if err != nil {
		panic(err)
	}
}
