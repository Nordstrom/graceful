/*

Package graceful simplifies graceful shutdown of HTTP servers (Go 1.8+)

Installation

Just go get the package:

    go get -u github.com/Nordstrom/graceful

Usage

A small usage example

    package main

    import (
        "context"
        "log"
        "net/http"
        "os"
        "time"

        "github.com/Nordstrom/graceful"
    )

		func main() {
			mux := http.NewServeMux()
			mux.HandleFunc("/hello", func(res http.ResponseWriter, req *http.Request) {
				res.Write([]byte("Hello World"))
			})

			graceful.ListenAndServe(&http.Server{Addr: ":8080", Handler: mux})
		}
*/
package graceful

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Server is implemented by *http.Server
type Server interface {
	ListenAndServe() error
	Shutdowner
}

// TLSServer is implemented by *http.Server
type TLSServer interface {
	ListenAndServeTLS(string, string) error
	Shutdowner
}

// Shutdowner is implemented by *http.Server, and optionally by *http.Server.Handler
type Shutdowner interface {
	Shutdown(ctx context.Context) error
}

// LoggerIface is implemented by *log.Logger
type LoggerIface interface {
	Printf(format string, v ...interface{})
	Fatal(...interface{})
}

// logger is the logger used by the shutdown function
// (defaults to logging to ioutil.Discard)
var logger LoggerIface = log.New(ioutil.Discard, "", 0)

var (
	shutdownHooks []func()
	listenHooks   []func()
)

// signals is the channel used to signal shutdown
var signals chan os.Signal

// Timeout for context used in call to *http.Server.Shutdown
var Timeout = 15 * time.Second

// Format strings used by the logger
var (
	ListeningFormat       = "Listening on http://0.0.0.0%s\n"
	ShutdownFormat        = "\nServer shutdown with timeout: %s\n"
	ErrorFormat           = "Error: %v\n"
	FinishedFormat        = "Shutdown finished %ds before deadline\n"
	FinishedHTTP          = "Finished all in-flight HTTP requests\n"
	HandlerShutdownFormat = "Shutting down handler with timeout: %ds\n"
)

type options struct {
	logger        LoggerIface
	shutdownHooks []func()
	listenHooks   []func()
}

// Option is a function that sets an option
type Option func(*options)

// Logger sets the logger option
func Logger(logger LoggerIface) Option {
	return func(opts *options) {
		opts.logger = logger
	}
}

// OnShutdown adds a shutdown hook function to the options
func OnShutdown(shutdownHook func()) Option {
	return func(opts *options) {
		opts.shutdownHooks = append(opts.shutdownHooks, shutdownHook)
	}
}

// OnListening adds a listen hook function to the options
func OnListening(listenHook func()) Option {
	return func(opts *options) {
		opts.listenHooks = append(opts.listenHooks, listenHook)
	}
}

func setOptions(o []Option) {
	var opts = &options{}
	for _, fn := range o {
		fn(opts)
	}
	if opts.logger != nil {
		logger = opts.logger
	}
	shutdownHooks = opts.shutdownHooks
}

// ListenAndServe starts the server in a goroutine and then calls Shutdown
func ListenAndServe(s Server, o ...Option) {
	setOptions(o)
	go func() {
		if err := s.ListenAndServe(); err != http.ErrServerClosed {
			logger.Fatal(err)
		}
		runListenHooks()
	}()

	Shutdown(s)
}

// ListenAndServeTLS starts the server in a goroutine and then calls Shutdown
func ListenAndServeTLS(s TLSServer, certFile, keyFile string, o ...Option) {
	setOptions(o)
	go func() {
		if err := s.ListenAndServeTLS(certFile, keyFile); err != http.ErrServerClosed {
			logger.Fatal(err)
		}
		runListenHooks()
	}()

	Shutdown(s)
}

func runListenHooks() {
	for _, hook := range listenHooks {
		hook()
	}
}

// Shutdown blocks until os.Interrupt or syscall.SIGTERM received, then
// running *http.Server.Shutdown with a context having a timeout
func Shutdown(s Shutdowner) {
	signals = make(chan os.Signal, 1)

	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	<-signals

	for _, hook := range shutdownHooks {
		hook()
	}
	shutdown(s, logger)
}

func shutdown(s Shutdowner, logger LoggerIface) {
	if s == nil {
		return
	}

	if logger == nil {
		logger = log.New(ioutil.Discard, "", 0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	logger.Printf(ShutdownFormat, Timeout)

	if err := s.Shutdown(ctx); err != nil {
		logger.Printf(ErrorFormat, err)
	} else {
		if hs, ok := s.(*http.Server); ok {
			logger.Printf(FinishedHTTP)

			if hss, ok := hs.Handler.(Shutdowner); ok {
				select {
				case <-ctx.Done():
					if err := ctx.Err(); err != nil {
						logger.Printf(ErrorFormat, err)
						return
					}
				default:
					if deadline, ok := ctx.Deadline(); ok {
						secs := (deadline.Sub(time.Now()) + time.Second/2) / time.Second
						logger.Printf(HandlerShutdownFormat, secs)
					}

					done := make(chan error)

					go func() {
						select {
						case <-ctx.Done():
							done <- ctx.Err()
						}
					}()

					go func() {
						done <- hss.Shutdown(ctx)
					}()

					if err := <-done; err != nil {
						logger.Printf(ErrorFormat, err)
						return
					}
				}
			}
		}

		if deadline, ok := ctx.Deadline(); ok {
			secs := (deadline.Sub(time.Now()) + time.Second/2) / time.Second
			logger.Printf(FinishedFormat, secs)
		}
	}
}
