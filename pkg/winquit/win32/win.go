//go:build windows
// +build windows

package win32

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type WNDCLASSEX struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	menuName      *uint16
	className     *uint16
	hIconSm       syscall.Handle
}

const (
	COLOR_WINDOW  = 0x05
	CW_USEDEFAULT = ^0x7fffffff
)

var (
	procEnumThreadWindows            = user32.NewProc("EnumThreadWindows")
	procRegisterClassEx              = user32.NewProc("RegisterClassExW")
	procCreateWindowEx               = user32.NewProc("CreateWindowExW")
	procDefWinProc                   = user32.NewProc("DefWindowProcW")
	procSetProcessShutdownParameters = kernel32.NewProc("SetProcessShutdownParameters")
	procShutdownBlockReasonCreate    = user32.NewProc("ShutdownBlockReasonCreate")

	callbackEnumThreadWindows = syscall.NewCallback(wndProcCloseWindow)

	OnEndSession      func(hWnd syscall.Handle)
	OnQueryEndSession func(hWnd syscall.Handle) bool

	enableAutoKillDescendantsOnce sync.Once
	enableAutoKillDescendantsErr  error
)

func DefWindowProc(hWnd syscall.Handle, msg uint32, wParam uintptr, lParam uintptr) int32 {

	ret, _, _ :=
		procDefWinProc.Call( // LRESULT DefWindowProcW()
			uintptr(hWnd), //          [in] HWND   hWnd,
			uintptr(msg),  //          [in] UINT   Msg,
			wParam,        //          [in] WPARAM wParam,
			lParam,        //          [in] LPARAM lParam
		)
	return int32(ret)
}

func GetModuleHandle(name string) (syscall.Handle, error) {
	var name16 *uint16
	var err error

	if len(name) > 0 {
		if name16, err = syscall.UTF16PtrFromString(name); err != nil {
			return 0, err
		}
	}

	ret, _, err :=
		procGetModuleHandle.Call( //     HMODULE GetModuleHandleW()
			uintptr(unsafe.Pointer(name16)), //  [in, optional] LPCWSTR lpModuleName
		)
	if ret == 0 {
		return 0, fmt.Errorf("could not obtain module handle: %w", err)
	}

	return syscall.Handle(ret), nil
}

func RegisterClassEx(class *WNDCLASSEX) (uint16, error) {

	ret, _, err :=
		procRegisterClassEx.Call( //      ATOM RegisterClassExW()
			uintptr(unsafe.Pointer(class)), // [in] const WNDCLASSEXW *unnamedParam1
		)
	if ret == 0 {
		return 0, fmt.Errorf("could not register window class: %w", err)
	}

	return uint16(ret), nil
}

func wndProc(hWnd syscall.Handle, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case WM_DESTROY:
		PostQuitMessage(0)
		return 0
	case WM_QUERYENDSESSION:
		if OnQueryEndSession != nil {
			if OnQueryEndSession(hWnd) {
				return uintptr(1)
			}
			return uintptr(0)
		}
	case WM_ENDSESSION:
		OnEndSession(hWnd)
		return 0
	}
	return uintptr(DefWindowProc(hWnd, msg, wParam, lParam))
}

func CloseThreadWindows(threadId uint32) bool {
	var hasWindow bool
	ret, _, _ :=
		procEnumThreadWindows.Call( // // BOOL EnumThreadWindows()
			uintptr(threadId),                   //      [in] DWORD       dwThreadId,
			callbackEnumThreadWindows,           //      [in] WNDENUMPROC lpfn,
			uintptr(unsafe.Pointer(&hasWindow)), //      [in] LPARAM      lParam
		)
	return ret != 0 && hasWindow
}

func wndProcCloseWindow(hwnd uintptr, lparam uintptr) uintptr {
	SendMessage(syscall.Handle(hwnd), WM_CLOSE, 0, 0)
	hasWindow := (*bool)(unsafe.Pointer(lparam))
	*hasWindow = true
	return 1
}

func RegisterDummyWinClass(name string, appInstance syscall.Handle) (uint16, error) {
	var class16 *uint16
	var err error
	if class16, err = syscall.UTF16PtrFromString(name); err != nil {
		return 0, err
	}

	class := WNDCLASSEX{
		className:   class16,
		hInstance:   appInstance,
		lpfnWndProc: syscall.NewCallback(wndProc),
	}

	class.cbSize = uint32(unsafe.Sizeof(class))

	return RegisterClassEx(&class)
}

func CreateDummyWindow(name string, className string, appInstance syscall.Handle) (syscall.Handle, error) {
	var name16, class16 *uint16
	var err error

	cwDefault := CW_USEDEFAULT

	if name16, err = syscall.UTF16PtrFromString(name); err != nil {
		return 0, err
	}
	if class16, err = syscall.UTF16PtrFromString(className); err != nil {
		return 0, err
	}
	ret, _, err := procCreateWindowEx.Call( //HWND CreateWindowExW(
		0,                                //       [in]           DWORD     dwExStyle,
		uintptr(unsafe.Pointer(class16)), //       [in, optional] LPCWSTR   lpClassName,
		uintptr(unsafe.Pointer(name16)),  //       [in, optional] LPCWSTR   lpWindowName,
		0,                                //       [in]           DWORD     dwStyle,
		uintptr(cwDefault),               //       [in]           int       X,
		uintptr(cwDefault),               //       [in]           int       Y,
		0,                                //       [in]           int       nWidth,
		0,                                //       [in]           int       nHeight,
		0,                                //       [in, optional] HWND      hWndParent,
		0,                                //       [in, optional] HMENU     hMenu,
		uintptr(appInstance),             //       [in, optional] HINSTANCE hInstance,
		0,                                //       [in, optional] LPVOID    lpParam
	)

	if ret == 0 {
		return 0, fmt.Errorf("could not create window: %w", err)
	}

	return syscall.Handle(ret), nil
}

func SetProcessShutdownParameters(level int, flags int) error {
	ret, _, err := procSetProcessShutdownParameters.Call(uintptr(level), uintptr(flags))
	if ret == 0 {
		return fmt.Errorf("SetProcessShutdownParameters failed: %w", err)
	}
	return nil
}

func ShutdownBlockReasonCreate(hWnd syscall.Handle, reason string) error {
	reasonPtr, err := syscall.UTF16PtrFromString(reason)
	if err != nil {
		return err
	}
	ret, _, err := procShutdownBlockReasonCreate.Call(
		uintptr(hWnd),
		uintptr(unsafe.Pointer(reasonPtr)),
	)
	if ret == 0 {
		return fmt.Errorf("ShutdownBlockReasonCreate failed: %w", err)
	}
	return nil
}

// EnableAutoKillDescendants enable automatically kill descendants after process terminated.
//
// Useful for guarantees program restart properly, as handles inherit by orphan
// descendants will cause resource competition. The job object handle was
// intentionally not closed, it will closed by the OS when current process
// terminated. Current process will associate with the job object, and
// descendant processes will inherit this job associate, and its life cycle
// determined by the job object too.
func EnableAutoKillDescendants() error {
	enableAutoKillDescendantsOnce.Do(func() {
		var jobHandle windows.Handle
		jobHandle, enableAutoKillDescendantsErr = windows.CreateJobObject(nil, nil)
		if enableAutoKillDescendantsErr != nil {
			enableAutoKillDescendantsErr = fmt.Errorf("CreateJobObject failed: %w", enableAutoKillDescendantsErr)
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		_, enableAutoKillDescendantsErr = windows.SetInformationJobObject(
			jobHandle,
			windows.JobObjectExtendedLimitInformation,
			(uintptr)(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		)
		if enableAutoKillDescendantsErr != nil {
			windows.CloseHandle(jobHandle)
			enableAutoKillDescendantsErr = fmt.Errorf("SetInformationJobObject failed: %w", enableAutoKillDescendantsErr)
			return
		}
		enableAutoKillDescendantsErr = windows.AssignProcessToJobObject(jobHandle, windows.CurrentProcess())
		if enableAutoKillDescendantsErr != nil {
			windows.CloseHandle(jobHandle)
			enableAutoKillDescendantsErr = fmt.Errorf("AssignProcessToJobObject failed: %w", enableAutoKillDescendantsErr)
			return
		}
	})
	return enableAutoKillDescendantsErr
}
