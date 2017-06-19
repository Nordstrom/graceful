package graceful

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Shutdowner is implemented by *http.Server
type Shutdowner interface {
	Shutdown(ctx context.Context) error
}

// Logger is implemented by *log.Logger
type Logger interface {
	Printf(format string, v ...interface{})
}

// DefaultTimeout for context used in call to *http.Server.Shutdown
var DefaultTimeout = 15 * time.Second

// DefaultLogger is the logger used by the shutdown function
var DefaultLogger = log.New(os.Stdout, "", 0)

// Format strings used by the logger
var (
	ShutdownFormat = "\nShutdown with timeout: %s\n"
	ErrorFormat    = "Error: %v\n"
	StoppedFormat  = "Server stopped\n"
)

// Shutdown blocks until os.Interrupt or syscall.SIGTERM received, then
// running *http.Server.Shutdown with a context having a timeout
func Shutdown(s Shutdowner) {
	wait()

	shutdown(s, DefaultLogger, DefaultTimeout)
}

func wait() {
	c := make(chan os.Signal, 1)

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
}

func shutdown(s Shutdowner, logger Logger, timeout time.Duration) {
	if s == nil {
		return
	}

	if logger == nil {
		logger = log.New(ioutil.Discard, "", 0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logger.Printf(ShutdownFormat, timeout)

	if err := s.Shutdown(ctx); err != nil {
		logger.Printf(ErrorFormat, err)
	} else {
		logger.Printf(StoppedFormat)
	}
}