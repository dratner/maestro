package logx

import (
	"fmt"
	"log"
	"os"
	"time"
)

type Logger struct {
	agentID string
	logger  *log.Logger
}

type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

func NewLogger(agentID string) *Logger {
	return &Logger{
		agentID: agentID,
		logger:  log.New(os.Stderr, "", 0), // Log to stderr for CLI compatibility
	}
}

func (l *Logger) log(level Level, format string, args ...any) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] [%s] %s: %s", timestamp, l.agentID, level, message)
	l.logger.Println(logLine)
}

func (l *Logger) Debug(format string, args ...any) {
	l.log(LevelDebug, format, args...)
}

func (l *Logger) Info(format string, args ...any) {
	l.log(LevelInfo, format, args...)
}

func (l *Logger) Warn(format string, args ...any) {
	l.log(LevelWarn, format, args...)
}

func (l *Logger) Error(format string, args ...any) {
	l.log(LevelError, format, args...)
}

func (l *Logger) GetAgentID() string {
	return l.agentID
}

func (l *Logger) WithAgentID(agentID string) *Logger {
	return &Logger{
		agentID: agentID,
		logger:  l.logger,
	}
}
