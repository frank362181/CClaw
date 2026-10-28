package exception

import (
	"errors"
	"fmt"
	"runtime"
)

type FilePathErrorDescription struct {
	ErrorDescription
}

var FilePathError = &FilePathErrorDescription{
	ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdSysFilePath,
		FileName: "",
		LineNo:   -1,
	},
}

func FilePathErrorEx(errTxt string) *FilePathErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &FilePathErrorDescription{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdSysFilePath,
			FileName: file,
			LineNo:   line,
		},
	}
}

func FilePathErrorEx2(errTxt string, a ...interface{}) *FilePathErrorDescription {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &FilePathErrorDescription{
		ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdSysFilePath,
			FileName: file,
			LineNo:   line,
		},
	}
}

func (e *FilePathErrorDescription) Error() string {
	return fmt.Sprintf("ERROR: [%s:%d]:%d: %s", e.FileName, e.LineNo, e.ErrId, e.ErrTxt)
}

func (e *FilePathErrorDescription) Is(target error) bool {
	if ed, ok := errors.AsType[*FilePathErrorDescription](target); ok {
		return ed.ErrId == e.ErrId
	}
	return false
}

var FileExtensionError = &FilePathErrorDescription{
	ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdSysFileExtension,
		FileName: "",
		LineNo:   -1,
	},
}

func FileExtensionErrorEx(errTxt string) *FilePathErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &FilePathErrorDescription{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdSysFileExtension,
			FileName: file,
			LineNo:   line,
		},
	}
}

var UnloadFileError = &FilePathErrorDescription{
	ErrorDescription{
		ErrTxt:   "",
		ErrId:    ErrorIdSysUnloadFile,
		FileName: "",
		LineNo:   -1,
	},
}

func UnloadFileErrorEx(errTxt string) *FilePathErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &FilePathErrorDescription{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    ErrorIdSysUnloadFile,
			FileName: file,
			LineNo:   line,
		},
	}
}

func UnloadFileErrorEx2(errTxt string, a ...interface{}) *FilePathErrorDescription {
	argsString := fmt.Sprintf(errTxt, a...)
	_, file, line, _ := runtime.Caller(1)
	return &FilePathErrorDescription{
		ErrorDescription{
			ErrTxt:   argsString,
			ErrId:    ErrorIdSysUnloadFile,
			FileName: file,
			LineNo:   line,
		},
	}
}
