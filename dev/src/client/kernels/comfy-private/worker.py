"""Entry point for an explicitly owned isolated Python interpreter only."""
from __future__ import annotations

import os
from pathlib import Path
import sys


def main():
    # -I omits the script directory from sys.path; add only this independently
    # pinned bridge directory, never a user's working directory or PYTHONPATH.
    if not sys.flags.isolated or not sys.dont_write_bytecode or not sys.flags.no_site or len(sys.argv) != 3:
        return 2
    bridge = Path(__file__).resolve().parent
    python_root = bridge.parent / 'python'
    if not Path(sys.executable).resolve().is_relative_to(python_root):
        return 2
    # -S prevents .pth/sitecustomize code before bundle verification. Add only
    # the fixed interpreter's package directory, without site.addsitedir().
    package_dir = (python_root / 'Lib' / 'site-packages') if os.name == 'nt' else (
        python_root / 'lib' / ('python' + str(sys.version_info.major) + '.' + str(sys.version_info.minor)) / 'site-packages')
    sys.path.insert(0, str(package_dir))
    sys.path.insert(0, str(bridge))
    from mesh_comfy.wire import redirect_diagnostics
    output = redirect_diagnostics()
    try:
        option, value = sys.argv[1:]
        if not value.isascii() or not value.isdecimal():
            return 2
        handle = int(value)
        if os.name == 'nt' and option == '--control-handle':
            import msvcrt
            descriptor = msvcrt.open_osfhandle(handle, os.O_RDONLY | os.O_BINARY)
        elif os.name != 'nt' and option == '--control-fd' and handle == 3:
            descriptor = handle
        else:
            return 2
        with os.fdopen(descriptor, 'rb', buffering=0) as control:
            from mesh_comfy.worker import run
            return run(sys.stdin.buffer, control, output)
    except BaseException:
        return 1
    finally:
        output.close()


if __name__ == '__main__':
    raise SystemExit(main())
