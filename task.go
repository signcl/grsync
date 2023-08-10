package grsync

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
)

// Task is high-level API under rsync
type Task struct {
	rsync *Rsync

	state *State
	log   *Log
}

// State contains information about rsync process
type State struct {
	Remain   int     `json:"remain"`
	Total    int     `json:"total"`
	Speed    string  `json:"speed"`
	Progress float64 `json:"progress"`
	Filename string  `json:"filename"`

	TransferedBytes   int64 `json:"transfered_bytes"`
	TransferedPercent int   `json:"transfered_percent"` // 0 ~ 100
}

// Log contains raw stderr and stdout outputs
type Log struct {
	Stderr string `json:"stderr"`
	Stdout string `json:"stdout"`
}

// State returns inforation about rsync processing task
func (t Task) State() State {
	return *t.state
}

// Log return structure which contains raw stderr and stdout outputs
func (t Task) Log() Log {
	return Log{
		Stderr: t.log.Stderr,
		Stdout: t.log.Stdout,
	}
}

// String return the actual exec cmd string of the task
func (t Task) String() string {
	return t.rsync.cmd.String()
}

// Run starts rsync process with options
func (t *Task) Run() error {
	stderr, err := t.rsync.StderrPipe()
	if err != nil {
		return err
	}
	defer stderr.Close()

	stdout, err := t.rsync.StdoutPipe()
	if err != nil {
		return err
	}
	defer stdout.Close()

	var wg sync.WaitGroup
	go processStdout(&wg, t, stdout)
	go processStderr(&wg, t, stderr)
	wg.Add(2)

	err = t.rsync.Run()
	wg.Wait()

	return err
}

// NewTask returns new rsync task
func NewTask(source, destination string, rsyncOptions RsyncOptions) *Task {
	// Force set required options
	rsyncOptions.HumanReadable = true
	rsyncOptions.Partial = true
	rsyncOptions.Archive = true

	return &Task{
		rsync: NewRsync(source, destination, rsyncOptions),
		state: &State{},
		log:   &Log{},
	}
}

func scanProgressLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\n' {
			// We have a line terminated by single newline.
			return i + 1, data[0:i], nil
		}
		advance = i + 1
		if len(data) > i+1 && data[i+1] == '\n' {
			advance += 1
		}
		return advance, data[0:i], nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}

func processStdout(wg *sync.WaitGroup, task *Task, stdout io.Reader) {
	const maxPercents = float64(100)
	const minDivider = 1

	defer wg.Done()

	progressMatcher := newMatcher(`\(.+-chk=(\d+.\d+)`)
	speedMatcher := newMatcher(`(\d+\.\d+.{2}\/s)`)
	transferedMatcher := newMatcher(`(\S+.*)%`)

	// Extract data from strings:
	//         999,999 99%  999.99kB/s    0:00:59 (xfr#9, to-chk=999/9999)
	//          2.39G  68%  659.73MB/s    0:00:03 (xfr#7217, to-chk=1113/10003)
	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanProgressLines)
	for scanner.Scan() {
		logStr := scanner.Text()
		if progressMatcher.Match(logStr) {
			task.state.Remain, task.state.Total = getTaskProgress(progressMatcher.Extract(logStr))

			copiedCount := float64(task.state.Total - task.state.Remain)
			task.state.Progress = copiedCount / math.Max(float64(task.state.Total), float64(minDivider)) * maxPercents
		}

		if speedMatcher.Match(logStr) {
			task.state.Speed = getTaskSpeed(speedMatcher.ExtractAllStringSubmatch(logStr, 2))
		}

		if transferedMatcher.Match(logStr) {
			task.state.TransferedBytes, task.state.TransferedPercent = getTaskTransfered(transferedMatcher.Extract(logStr))
		}
		if isFilename(logStr) {
			task.state.Filename = logStr
		}

		task.log.Stdout += logStr + "\n"
	}

}

func processStderr(wg *sync.WaitGroup, task *Task, stderr io.Reader) {
	defer wg.Done()

	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		task.log.Stderr += scanner.Text() + "\n"
	}
}

func getTaskProgress(remTotalString string) (int, int) {
	const remTotalSeparator = "/"
	const numbersCount = 2
	const (
		indexRem = iota
		indexTotal
	)

	info := strings.Split(remTotalString, remTotalSeparator)
	if len(info) < numbersCount {
		return 0, 0
	}

	remain, _ := strconv.Atoi(info[indexRem])
	total, _ := strconv.Atoi(info[indexTotal])

	return remain, total
}

func getTaskSpeed(data [][]string) string {
	if len(data) < 1 || len(data[0]) < 1 {
		return ""
	}

	return data[0][0]
}

func getTaskTransfered(transfered string) (transferedBytes int64, transferedPercent int) {
	info := strings.Split(transfered, " ")
	if len(info) < 2 {
		return 0, 0
	}
	numberStr := info[0]
	percentStr := info[len(info)-1]

	transferedPercent, _ = strconv.Atoi(percentStr)

	var unit string
	var number float64

	units := []string{"KB", "K", "MB", "M", "GB", "G", "TB", "T"}
	for _, u := range units {
		s := info[0]

		if strings.HasSuffix(s, u) {
			unit = u
			numberStr = s[:len(s)-len(u)]
			break
		}
	}

	numberStr = strings.ReplaceAll(numberStr, ",", "")
	numberStr = strings.TrimSpace(numberStr)
	number, _ = strconv.ParseFloat(numberStr, 64)

	switch unit {
	case "KB", "K":
		transferedBytes = int64(number * 1024)
	case "MB", "M":
		transferedBytes = int64(number * 1024 * 1024)
	case "GB", "G":
		transferedBytes = int64(number * 1024 * 1024 * 1024)
	case "TB", "T":
		transferedBytes = int64(number * 1024 * 1024 * 1024 * 1024)
	default:
		transferedBytes = int64(number)
	}

	return
}

// # Call this if you want to filter out verbose messages (-v or -vv) from
// # the output of an rsync run (whittling the output down to just the file
// # messages).  This isn't needed if you use -i without -v.
// filter_outfile() {
//     sed -e '/^building file list /d' \
// 	-e '/^sending incremental file list/d' \
// 	-e '/^created directory /d' \
// 	-e '/^done$/d' \
// 	-e '/ --whole-file$/d' \
// 	-e '/^total: /d' \
// 	-e '/^client charset: /d' \
// 	-e '/^server charset: /d' \
// 	-e '/^$/,$d' \
// 	<"$outfile" >"$outfile.new"
//     mv "$outfile.new" "$outfile"
// }

func isFilename(str string) bool {

	if str == "" {
		return false
	}

	if strings.HasPrefix(str, "      ") {
		return false
	}

	if strings.HasPrefix(str, " ") {
		return false
	}

	verbose := []string{
		"building file list ",
		"sending ",
		"created ",
		"done",
		"total ",
		"total: ",
		"client ",
		"server ",
		"to consider",
		"to-chk=",
		"to-check=",
	}

	for _, v := range verbose {
		if strings.Contains(str, v) {
			return false
		}
	}

	if strings.HasPrefix(str, "sent") {
		return false
	}

	if strings.HasSuffix(str, "/") {
		return false
	}

	return true
}
