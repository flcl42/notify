package qr

import (
	"fmt"
	"strings"

	"github.com/skip2/go-qrcode"
)

func PrintTerminal(text string) error {
	qr, err := qrcode.New(text, qrcode.Low)
	if err != nil {
		return err
	}
	// Use small ASCII rendering: each block is two pixels high.
	lines := strings.Split(qr.ToSmallString(false), "\n")
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}
