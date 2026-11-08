// Package daemon keeps a single child process running: it restarts the child
// when it exits and stops it when the parent stops or dies.
package daemon

import (
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// For the binary to be used as a daemon worker this flag need to be the first argument
const DaemonWorkerFlagname = "--daemon-worker-hidden"

// Watch daemon to prevent running children when parent dies
func init() {
	args := os.Args[1:]
	if len(args) < 2 || args[0] != DaemonWorkerFlagname {
		return
	}

	ppid, err := strconv.Atoi(args[1])
	if err != nil {
		log.Fatal("Expecting a valid integer as ppid")
	}

	input := args[2:]
	if len(input) == 0 {
		log.Fatal("No executable defined for the main worker")
	}

	// Registered before the child starts: a signal arriving earlier would
	// kill the worker with the default action and orphan the child.
	var notifyCh = make(chan os.Signal, 1)
	signal.Notify(notifyCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	executable := input[0]
	executableArgs := input[1:]

	log.Printf("Executing: %s %s", executable, executableArgs)

	var parentDied = make(chan struct{})
	go func() {
		defer func() { recover() }()
		defer close(parentDied)

		ticker := time.NewTicker(time.Second)
		for range ticker.C {
			if ppid != os.Getppid() {
				return
			}
		}
	}()

	var childDied = make(chan int, 1)
	cmd := exec.Command(executable, executableArgs...)
	cmd.WaitDelay = 10 * time.Second
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Start()
	if err != nil {
		log.Fatal("Failed to start child process")
	}
	go func() { cmd.Wait(); childDied <- cmd.ProcessState.ExitCode() }()

	exitCode := func() (exitcode int) {
		select {
		case sig := <-notifyCh:
			log.Println("Process signaled to finish")
			err = cmd.Process.Signal(sig)
			if err != nil {
				log.Fatal("Failed to signal child process")
			}

			select {
			case exitCode := <-childDied:
				return exitCode
			case <-time.After(5 * time.Second):
				err = cmd.Process.Kill()
				if err != nil {
					log.Fatal("Failed to kill child process")
				}
				return <-childDied
			}
		case <-parentDied:
			log.Println("Parent died")
			err = cmd.Process.Signal(syscall.SIGTERM)
			if err != nil {
				log.Fatal("Failed to signal SIGTERM to child process")
			}

			select {
			case exitCode := <-childDied:
				return exitCode
			case <-time.After(5 * time.Second):
				err = cmd.Process.Kill()
				if err != nil {
					log.Fatal("Failed to kill child process")
				}
				return <-childDied
			}
		case exitCode := <-childDied:
			return exitCode
		}
	}()

	os.Exit(exitCode)
}

type Config struct {
	// MANDATORY Parent context used to cleanly stop the process when done
	ControlContext context.Context
	// Interval between restarts
	// It is shared with error interval
	Interval time.Duration
	// Expiration date based on which the process will auto stop and not boot anymore
	ExpiresAt time.Time
	// Program to execute
	Executable string
	// Program arguments
	Arguments []string
	// Program working directory
	WorkingDirectory string

	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

type Daemon struct {
	killCh     chan struct{}
	notifyKill chan struct{}
}

func (d *Daemon) Kill(ctx context.Context) {
	defer func() { recover() }()
	close(d.killCh)

	select {
	case <-ctx.Done():
	case <-d.notifyKill:
	}
}

// Starts the provided executable as a daemon
func New(ctx context.Context, c *Config) (d *Daemon) {
	// Used to notify a daemon kill
	var killCh = make(chan struct{})
	// Used to notify that a daemon was completely shutdown
	var notifyKill = make(chan struct{})

	interval := max(2*time.Second, c.Interval)

	// os.Args[0] may be relative or a bare name, which breaks once the
	// caller changes directory.
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}

	go func() {
		defer func() { recover() }()

		logger := slog.Default().
			WithGroup("Daemon").
			With("interval", interval).
			With("expires-at", c.ExpiresAt).
			With("executable", c.Executable).
			With("args[]", c.Arguments).
			With("workdir", c.WorkingDirectory)

		// Always notify when the daemon died
		defer func() { logger.InfoContext(ctx, "Daemon finished"); close(notifyKill) }()
		// Interval channel is used as prevention mechanism from fork bombs
		var intervalCh = time.NewTicker(interval)
		defer intervalCh.Stop()

		logger.InfoContext(ctx, "Starting loop")
		for {
			select {
			case <-killCh:
				return
			case <-c.ControlContext.Done():
				return
			default:
			}

			if !c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt) {
				return
			}

			retry := func() (retry bool) {
				defer func() { recover() }()

				var daemonCtx context.Context
				if c.ExpiresAt.IsZero() {
					daemonCtx = c.ControlContext
				} else {
					var cancel func()
					daemonCtx, cancel = context.WithDeadline(c.ControlContext, c.ExpiresAt)
					defer cancel()
				}

				var args = make([]string, 0, 3+len(c.Arguments))
				args = append(args, DaemonWorkerFlagname, strconv.Itoa(os.Getpid()), c.Executable)
				args = append(args, c.Arguments...)

				cmd := exec.Command(self, args...)
				cmd.Dir = c.WorkingDirectory
				cmd.Stdin = c.Stdin
				cmd.Stdout, cmd.Stderr = c.Stdout, c.Stderr
				err := cmd.Start()
				if err != nil {
					logger.With("error-msg", err).WarnContext(ctx, "failed to start executable with args")
					return true
				}

				logger.InfoContext(ctx, "Process started successfully")

				var cmdFinishCh = make(chan struct{})
				go func() { cmd.Wait(); close(cmdFinishCh); logger.InfoContext(ctx, "Process finished") }()

				select {
				case <-daemonCtx.Done():
					// The parent context finished
					// We need to notify the child process to terminate
					// If it fails to stop until the deadline force a kill
					cmd.Process.Signal(syscall.SIGINT)
					<-cmdFinishCh
					return false
				case <-killCh:
					// Terminate the process inmediatly
					// And wait for it to actually exit
					cmd.Process.Signal(syscall.SIGINT)
					<-cmdFinishCh
					return false
				case <-cmdFinishCh:
					// The process actually exited by its own
					// We need to first sleep the require amount of time to prevent starting hundreds of processes
					return true
				}
			}()

			// If we finished
			// Kill the daemon entirely
			if !retry {
				return
			}

			// The first start is immediate; after an exit that is not
			// daemon triggered wait for the interval prior the next loop
			select {
			case <-intervalCh.C:
			case <-killCh:
				return
			case <-c.ControlContext.Done():
				return
			}
		}
	}()
	return &Daemon{
		killCh:     killCh,
		notifyKill: notifyKill,
	}
}
