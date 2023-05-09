package eureka

type LogFunc = func(format string, args ...interface{})

func noop(string, ...interface{}) {}

var _debugf LogFunc = noop
var _warningf LogFunc = noop
var _errorf LogFunc = noop

func RegisterLoggers(debugf, infof, warnf, errorf LogFunc) {
	_debugf = debugf
	_warningf = warnf
	_errorf = errorf
}
