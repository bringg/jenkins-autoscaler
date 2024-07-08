package schedule

import (
	"time"

	"github.com/adhocore/gronx"
)

type FakeSegmentChecker struct {
	checker gronx.Checker
}

func NewFakeSegmentChecker() *FakeSegmentChecker {
	checker := &gronx.SegmentChecker{}
	checker.SetRef(time.Date(2024, time.July, 1, 1, 1, 0, 0, time.UTC))

	return &FakeSegmentChecker{checker}
}

func (c *FakeSegmentChecker) GetRef() time.Time {
	return c.checker.GetRef()
}

func (c *FakeSegmentChecker) SetRef(_ time.Time) {}

func (c *FakeSegmentChecker) CheckDue(segment string, pos int) (due bool, err error) {
	return c.checker.CheckDue(segment, pos)
}
