package grsync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTask(t *testing.T) {
	t.Run("create new empty Task", func(t *testing.T) {
		createdTask := NewTask("a", "b", RsyncOptions{})

		assert.Empty(t, createdTask.Log(), "Task log should return empty string")
		assert.Empty(t, createdTask.State(), "Task should inited with empty state")
	})
}

func TestTaskProgressParse(t *testing.T) {
	progressMatcher := newMatcher(`\(.+-chk=(\d+.\d+)`)
	const taskInfoString = `999,999 99%  999.99kB/s    0:00:59 (xfr#9, to-chk=999/9999)`
	remain, total := getTaskProgress(progressMatcher.Extract(taskInfoString))

	assert.Equal(t, remain, 999)
	assert.Equal(t, total, 9999)
}

func TestTaskProgressWithDifferentChkID(t *testing.T) {
	progressMatcher := newMatcher(`\(.+-chk=(\d+.\d+)`)
	const taskInfoString = `999,999 99%  999.99kB/s    0:00:59 (xfr#9, ir-chk=999/9999)`
	remain, total := getTaskProgress(progressMatcher.Extract(taskInfoString))

	assert.Equal(t, remain, 999)
	assert.Equal(t, total, 9999)
}

func TestTaskSpeedParse(t *testing.T) {
	speedMatcher := newMatcher(`(\d+\.\d+.{2}\/s)`)
	const taskInfoString = `999,999 99%  999.99kB/s    0:00:59 (xfr#9, ir-chk=999/9999)`
	speed := getTaskSpeed(speedMatcher.ExtractAllStringSubmatch(taskInfoString, 2))
	assert.Equal(t, "999.99kB/s", speed)
}

func TestTaskTransfered(t *testing.T) {
	transferedMatcher := newMatcher(`(\S+.*)%`)

	tests := []struct {
		info          string
		expectSize    int64
		expectPercent int
	}{
		{
			info:          `123,456 78%  87.65kB/s    0:00:59 (xfr#9, to-chk=999/9999)`,
			expectSize:    int64(123456),
			expectPercent: 78,
		},
		{
			info:          `21.90G  98%  428.46MB/s    0:00:48 (xfr#9416, ir-chk=3383/13809)`,
			expectSize:    23514945945,
			expectPercent: 98,
		},
	}

	for _, tt := range tests {
		size, percent := getTaskTransfered(transferedMatcher.Extract(tt.info))
		assert.Equal(t, tt.expectSize, size)
		assert.Equal(t, tt.expectPercent, percent)
	}
}
