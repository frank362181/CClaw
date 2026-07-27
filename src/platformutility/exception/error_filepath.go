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
		ErrId:    errorIdFilePath,
		FileName: "",
		LineNo:   -1,
	},
}

func FilePathErrorEx(errTxt string) *FilePathErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &FilePathErrorDescription{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdFilePath,
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
		ErrId:    errorIdFileExtension,
		FileName: "",
		LineNo:   -1,
	},
}

func FileExtensionErrorEx(errTxt string) *FilePathErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &FilePathErrorDescription{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdFileExtension,
			FileName: file,
			LineNo:   line,
		},
	}
}

var UnloadFileError = &FilePathErrorDescription{
	ErrorDescription{
		ErrTxt:   "",
		ErrId:    errorIdUnloadFile,
		FileName: "",
		LineNo:   -1,
	},
}

func UnloadFileErrorEx(errTxt string) *FilePathErrorDescription {
	_, file, line, _ := runtime.Caller(1)
	return &FilePathErrorDescription{
		ErrorDescription{
			ErrTxt:   errTxt,
			ErrId:    errorIdUnloadFile,
			FileName: file,
			LineNo:   line,
		},
	}
}
