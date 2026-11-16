package tests

import (
	"testing"
	"time"
)

func TestSlow(t *testing.T) { time.Sleep(90 * time.Second) }
