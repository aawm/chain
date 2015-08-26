// Package log implements a standard convention for structured logging.
// Log entries are formatted as K=V pairs and written to stdout.
package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"

	"chain/net/http/reqid"
)

var (
	logWriterMu sync.Mutex // protects the following
	logWriter   io.Writer  = os.Stdout

	// pairDelims contains a list of characters that may be used as delimeters
	// between key-value pairs in a log entry. Keys and values will be quoted or
	// otherwise formatted to ensure that key-value extraction is unambiguous.
	//
	// The list of pair delimiters follows Splunk conventions, described here:
	// http://answers.splunk.com/answers/143368/default-delimiters-for-key-value-extraction.html
	pairDelims      = " ,;|&\t\n\r"
	illegalKeyChars = pairDelims + `="`
)

// Conventional key names for log entries
const (
	KeyCaller = "at"    // location of caller
	KeyTime   = "t"     // time of call
	KeyReqID  = "reqid" // request ID from context

	KeyMessage = "message" // produced by Message
	KeyError   = "error"   // produced by Error

	keyLogError = "log-error" // for errors produced by the log package itself
)

// Write writes a structured log entry to stdout. Log fields are
// specified as a variadic sequence of alternating keys and values.
//
// Duplicate keys will be preserved.
//
// Several fields are automatically added to the log entry: a timestamp, a
// string indicating the file and line number of the caller, and a request ID
// taken from the context.
//
// As a special case, the auto-generated caller may be overridden by passing in
// a new value for the KeyCaller key as the first key-value pair. The override
// feature should be reserved for custom logging functions that wrap Write.
func Write(ctx context.Context, keyvals ...interface{}) {
	// Invariant: len(keyvals) is always even.
	if len(keyvals)%2 != 0 {
		keyvals = append(keyvals, "", keyLogError, "odd number of log params")
	}

	// The auto-generated caller value may be overwritten.
	var vcaller interface{}
	if len(keyvals) >= 2 && keyvals[0] == KeyCaller {
		vcaller = keyvals[1]
		keyvals = keyvals[2:]
	} else {
		vcaller = caller(1)
	}

	// Prepend the log entry with auto-generated fields.
	out := fmt.Sprintf(
		"%s=%s %s=%s %s=%s",
		KeyReqID, formatValue(reqid.FromContext(ctx)),
		KeyCaller, formatValue(vcaller),
		KeyTime, formatValue(time.Now().UTC().Format(time.RFC3339)),
	)

	for i := 0; i < len(keyvals); i += 2 {
		k := formatKey(keyvals[i])
		v := formatValue(keyvals[i+1])
		out += " " + k + "=" + v
	}

	logWriterMu.Lock()
	logWriter.Write([]byte(out)) // ignore errors
	logWriterMu.Unlock()
}

// Messagef writes a log entry containing a message assigned to the
// "message" key. Arguments are handled as in fmt.Printf.
func Messagef(ctx context.Context, format string, a ...interface{}) {
	Write(ctx, KeyCaller, caller(1), KeyMessage, fmt.Sprintf(format, a...))
}

// Error writes a log entry containing an error message assigned to the
// "error" key.
// Optionally, an error message prefix can be included. Prefix arguments are
// handled as in fmt.Print.
func Error(ctx context.Context, err error, a ...interface{}) {
	var msg string
	if len(a) > 0 {
		msg = fmt.Sprint(a...) + ": " + err.Error()
	} else {
		msg = err.Error()
	}
	Write(ctx, KeyCaller, caller(1), KeyError, msg)
}

// caller returns a string containing filename and line number of a
// function invocation on the calling goroutine's stack.
// The argument skip is the number of stack frames to ascend, where
// 0 is the calling site of caller. If no stack information is not available,
// "?:?" is returned.
func caller(skip int) string {
	_, file, nline, ok := runtime.Caller(skip + 1)

	var line string
	if ok {
		file = filepath.Base(file)
		line = strconv.Itoa(nline)
	} else {
		file = "?"
		line = "?"
	}

	return file + ":" + line
}

// formatKey ensures that the stringified key is valid for use in a
// Splunk-style K=V format. It stubs out delimeter and quoter characters in
// the key string with hyphens.
func formatKey(k interface{}) string {
	s := fmt.Sprint(k)
	if s == "" {
		return "?"
	}

	for _, c := range illegalKeyChars {
		s = strings.Replace(s, string(c), "-", -1)
	}

	return s
}

// formatValue ensures that the stringified value is valid for use in a
// Splunk-style K=V format. It quotes the string value if delimeter or quoter
// characters are present in the value string.
func formatValue(v interface{}) string {
	s := fmt.Sprint(v)
	if strings.ContainsAny(s, pairDelims) {
		return strconv.Quote(s)
	}
	return s
}
