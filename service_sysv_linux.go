// Copyright 2015 Daniel Theophanes.
// Use of this source code is governed by a zlib-style
// license that can be found in the LICENSE file.package service

package service

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"os/exec"
	"syscall"
	"text/template"
	"time"
)

type sysv struct {
	i Interface
	*Config
}

func newSystemVService(i Interface, c *Config) (Service, error) {
	s := &sysv{
		i:      i,
		Config: c,
	}

	return s, nil
}

func (s *sysv) String() string {
	if len(s.DisplayName) > 0 {
		return s.DisplayName
	}
	return s.Name
}

var errNoUserServiceSystemV = errors.New("User services are not supported on SystemV.")

func (s *sysv) configPath() (cp string, err error) {
	if s.Option.bool(optionUserService, optionUserServiceDefault) {
		err = errNoUserServiceSystemV
		return
	}
	cp = "/etc/init.d/" + s.Config.Name
	return
}

/* Determine the SysV flavour of this Linux system.
   Guidelines:
   1. if RH functions exist, make use of them, else
   2. if no LSB functions exist (even Debian/Ubuntu require them), exit with a message, else
   3. if start-stop-daemon is in $PATH, proceed as if it is a Debian-like system, else
   4. fall back to LSB functions to start/stop the service.
 */
func determineDistroFlavour() string {
	if _, err := os.Stat("/etc/rc.d/init.d/functions"); err == nil {
		/* Red Hat / CentOS type systems */
		return "redhat"
	} else if  _, err := os.Stat("/lib/lsb/init-functions"); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "System is lacking LSB support (/lib/lsb/init-functions)")
		os.Exit(1)
	} else if _, err := exec.LookPath("start-stop-daemon"); err == nil {
		/* debian/ubuntu based systems support this */
		return "debian"
	}
	/* Linux Standard Base as lowest common denominator */
	return "lsb"
}


func (s *sysv) Install() error {
	confPath, err := s.configPath()
	if err != nil {
		return err
	}

	path, err := s.execPath()
	if err != nil {
		return err
	}

	var to = &struct {
		*Config
		// Absolute path of the executable
		Path		string
		// The SysV flavour of this system
		Flavour		string
	}{
		s.Config,
		path,
		determineDistroFlavour(),
	}

	if _, err = os.Stat(confPath); err == nil {
		return fmt.Errorf("Init already exists: %s", confPath)
	}

	f, err := os.Create(confPath)
	if err != nil {
		return err
	}
	defer f.Close()

	err = template.Must(template.New("").Funcs(tf).Parse(sysvScript)).Execute(f, to)
	if err != nil {
		return err
	}

	if err = os.Chmod(confPath, 0755); err != nil {
		return err
	}
	for _, i := range [...]string{"2", "3", "4", "5"} {
		if err = os.Symlink(confPath, "/etc/rc"+i+".d/S50"+s.Name); err != nil {
			continue
		}
	}
	for _, i := range [...]string{"0", "1", "6"} {
		if err = os.Symlink(confPath, "/etc/rc"+i+".d/K02"+s.Name); err != nil {
			continue
		}
	}

	return nil
}

func (s *sysv) Uninstall() error {
	cp, err := s.configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(cp); err != nil {
		return err
	}
	return nil
}

func (s *sysv) Logger(errs chan<- error) (Logger, error) {
	if system.Interactive() {
		return ConsoleLogger, nil
	}
	return s.SystemLogger(errs)
}
func (s *sysv) SystemLogger(errs chan<- error) (Logger, error) {
	return newSysLogger(s.Name, errs)
}

func (s *sysv) Run() (err error) {
	err = s.i.Start(s)
	if err != nil {
		return err
	}

	sigChan := make(chan os.Signal, 3)

	signal.Notify(sigChan, syscall.SIGTERM, os.Interrupt)

	<-sigChan

	return s.i.Stop(s)
}

func (s *sysv) Start() error {
	return run("service", s.Name, "start")
}

func (s *sysv) Stop() error {
	return run("service", s.Name, "stop")
}

func (s *sysv) Restart() error {
	err := s.Stop()
	if err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return s.Start()
}

const sysvScript = `#!/bin/bash
{{ if eq .Flavour "redhat"}}#
# {{.DisplayName}}
#
# chkconfig:   2345 50 02
# description: {{.Description}}

# Source function library.
. /etc/rc.d/init.d/functions
{{else}}{{/* System with support for LSB */}}### BEGIN INIT INFO
# Provides:          {{.Path}}
# Required-Start:    $local_fs $remote_fs $network $syslog
# Required-Stop:     $local_fs $remote_fs $network $syslog
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: {{.DisplayName}}
# Description:       {{.Description}}
### END INIT INFO

# Source function library.
. /lib/lsb/init-functions{{end}}
CMD="{{.Path}}"
NAME="{{.Name}}"
DESC="{{.Description}}"

# The following can be overridden via configuration file
PIDFILE="/var/run/${NAME}.pid"
LOCKFILE="/var/lock/subsys/${NAME}"

# Log output of $CMD
STDOUTLOG="/dev/null"
STDERRLOG="/dev/null"

# Startup options
DAEMON_ARGS="{{range .Arguments}} {{.|cmd}}{{end}}"

# Source configuration defaults to override above as appropriate.
! test -e /etc/default/${NAME}   || . /etc/default/${NAME}
! test -e /etc/sysconfig/${NAME} || . /etc/sysconfig/${NAME}
test $(id -u) -eq "0"            || exit 4 # LSB exit: insufficient permissions
test -x ${CMD}                   || exit 5 # LSB exit: program not installed
test -d $(dirname $LOCKFILE)     || mkdir -p $(dirname $LOCKFILE)
{{ if eq .Flavour "redhat"}}
get_status() {
    status -p "$PIDFILE" "$CMD"
}

start() {
    get_status &>/dev/null && return 0
    echo -n $"Starting ${DESC}: "
    daemon --pidfile="$PIDFILE" {{if .UserName}}--user={{.UserName}}{{end}} \
	   "$CMD $DAEMON_ARGS </dev/null >$STDOUTLOG 2>$STDERRLOG & echo \$! > $PIDFILE"
    sleep 0.5 # wait briefly to see if service failed to start
    get_status &>/dev/null && success || failure
    RETVAL=$?
    [ $RETVAL -eq 0 ] &&  touch "$LOCKFILE" || rm -f "$PIDFILE"
    echo
    return $RETVAL
}

stop() {
    get_status &>/dev/null || return 0
    echo -n $"Stopping ${DESC}: "
    killproc -p "$PIDFILE" "$CMD" -TERM
    RETVAL=$?
    [ $RETVAL -eq 0 ] && rm -f "$LOCKFILE" "$PIDFILE" || rm -f "$PIDFILE"
    echo
    return $RETVAL
}{{else}}{{/* Debian-like system or fallback to LSB */}}
if type status_of_proc &>/dev/null; then	# newer LSB versions only
    get_status() {
        status_of_proc $([ -e $PIDFILE ] && echo -p $PIDFILE) "$CMD" "$NAME"
    }
else
    get_status() {
	pidofproc $([ -e $PIDFILE ] && echo -p $PIDFILE) "$CMD" >/dev/null
	RETVAL=$?
	if [ $RETVAL -eq 0 ]; then
	    log_success_msg "$NAME is running"
	elif [ $RETVAL -eq 4 ]; then
	    log_failure_msg "could not access PID file for $NAME"
	else
	    log_failure_msg "$NAME is not running"
	fi
	return $RETVAL
    }
fi
{{ if eq .Flavour "debian"}}
start() {
    log_daemon_msg "Starting ${DESC}"
    start-stop-daemon --start \
    {{if .ChRoot}}--chroot {{.ChRoot|cmd}}{{end}} \
    {{if .WorkingDirectory}}--chdir {{.WorkingDirectory|cmd}}{{end}} \
    {{if .UserName}}--chuid {{.UserName|cmd}}{{end}} \
    --pidfile "$PIDFILE" \
    --background \
    --make-pidfile \
    --exec "$CMD" -- "$DAEMON_ARGS"
    log_end_msg  $?
}

stop() {
    log_daemon_msg "Stopping ${DESC}"
    start-stop-daemon --stop --pidfile "$PIDFILE" --quiet
    RETVAL=$?
    rm -f "$PIDFILE"
    log_end_msg $RETVAL
}{{else}}{{/* LSB */}}
start() {
    get_status &>/dev/null && return 0
    echo -n $"Starting $DESC: ${NAME}"
    {{if .WorkingDirectory}}cd {{.WorkingDirectory|cmd}}{{end}}
    "$CMD" "$DAEMON_ARGS" </dev/null >"$STDOUTLOG" 2>"$STDERRLOG" &
    echo $! > "$PIDFILE"
    sleep 0.5 # wait briefly to see if service crashed
    get_status &>/dev/null
    RETVAL=$?
    if [ $RETVAL -eq 0 ]; then
	    log_success_msg
	    touch "$LOCKFILE"
    else
	    log_failure_msg
	    rm -f "$PIDFILE"
    fi
    return $RETVAL
}

stop() {
    get_status &>/dev/null || return 0
    echo -n $"Stopping ${DESC}: ${NAME}"
    killproc -p "$PIDFILE" "$CMD" -TERM
    RETVAL=$?
    if [ $RETVAL -eq 0 ]; then
	    log_success_msg
	    rm -f "$LOCKFILE"
    else
	    log_failure_msg
    fi
    rm -f "$PIDFILE"
    return $RETVAL
}{{end}}{{/* LSB */}}{{end}}{{/* Debian-like system */}}

case "$1" in
    start|stop)
	$1
	;;
    restart|force-reload)
	stop
	start
	;;
    status)
	get_status
	;;
    *)
	echo $"Usage: $0 {start|stop|status|restart|force-reload}" >&2
	exit 2 # LSB: invalid or excess arguments
esac
`
