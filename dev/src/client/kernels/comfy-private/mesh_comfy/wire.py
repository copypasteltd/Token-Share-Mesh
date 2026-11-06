"""Independent bounded private content/control frames; never HTTP or pickle."""
from __future__ import annotations

import os
import select
import struct
import threading

from .limits import CHUNK, Rejected, canonical, strict_json

INIT, INVOKE, ASSET_BEGIN, ASSET_DATA, ASSET_END, SHUTDOWN = range(1, 7)
READY, ACCEPTED, STATE, RESULT, STOPPED, CLOSED, ERROR = range(101, 108)
CANCEL, STOP_SESSION, AUTHORIZE = 201, 202, 203
MAX_FRAME = (2 << 20) + 1


class CancelledReader:
    """Poll only the owned pipe so input stalls cannot hide a local deadline."""
    def __init__(self, source, cancelled):
        self.source, self.cancelled = source, cancelled
        self.pause = threading.Event()
        if os.name == 'nt':
            import ctypes
            import msvcrt
            from ctypes import wintypes
            system = os.environ.get('SystemRoot')
            if not system:
                raise Rejected('ENVIRONMENT')
            library = ctypes.WinDLL(os.path.join(system, 'System32', 'kernel32.dll'), use_last_error=True, winmode=0x800)
            peek = library.PeekNamedPipe
            peek.argtypes = [wintypes.HANDLE, ctypes.c_void_p, wintypes.DWORD,
                             ctypes.c_void_p, ctypes.POINTER(wintypes.DWORD), ctypes.c_void_p]
            peek.restype = wintypes.BOOL
            self._windows = (ctypes, wintypes, peek, msvcrt.get_osfhandle(source.fileno()))

    def readinto(self, target):
        view = memoryview(target).cast('B')
        if not len(view):
            return 0
        while True:
            if self.cancelled():
                raise Rejected('CANCELLED')
            if os.name == 'nt':
                ctypes, wintypes, peek, handle = self._windows
                available = wintypes.DWORD()
                if not peek(handle, None, 0, None, ctypes.byref(available), None):
                    if ctypes.get_last_error() in (109, 232):
                        return 0
                    raise Rejected('UNKNOWN')
                if available.value:
                    return self.source.readinto(view[:min(len(view), available.value)])
                self.pause.wait(0.02)
            elif select.select([self.source.fileno()], [], [], 0.05)[0]:
                return self.source.readinto(view)


def exact(source, size, eof=False):
    data = bytearray(size)
    view = memoryview(data)
    done = 0
    while done < size:
        count = source.readinto(view[done:])
        if not count:
            if eof and done == 0:
                return None
            raise Rejected('UNKNOWN')
        if type(count) is not int or not 0 < count <= size - done:
            raise Rejected('UNKNOWN')
        done += count
    return bytes(data)


def read_frame(source, maximum=MAX_FRAME):
    size = exact(source, 4, eof=True)
    if size is None:
        return None
    count, = struct.unpack('<I', size)
    if not 1 <= count <= maximum:
        raise Rejected('LIMIT')
    data = exact(source, count)
    return data[0], data[1:]


def read_json(source, kind, maximum=2 << 20):
    frame = read_frame(source, maximum + 1)
    if frame is None or frame[0] != kind:
        raise Rejected('FORMAT')
    return strict_json(frame[1], maximum)


class Writer:
    def __init__(self, output):
        self._output = output
        self._lock = threading.Lock()

    def send(self, kind, value):
        raw = canonical(value)
        if len(raw) > CHUNK or kind not in range(101, 108):
            raise Rejected('LIMIT')
        with self._lock:
            data = struct.pack('<IB', len(raw) + 1, kind) + raw
            view = memoryview(data)
            while view:
                written = self._output.write(view)
                if type(written) is not int or not 0 < written <= len(view):
                    raise Rejected('UNKNOWN')
                view = view[written:]
            self._output.flush()


def redirect_diagnostics():
    # Keep protocol handles private before silencing Python/C-library stdout
    # and stderr. No prompt/graph/exception text may mix with protocol frames.
    output = os.fdopen(os.dup(1), 'wb', buffering=0)
    null = os.open(os.devnull, os.O_RDWR)
    try:
        os.dup2(null, 1)
        os.dup2(null, 2)
    finally:
        os.close(null)
    return output
