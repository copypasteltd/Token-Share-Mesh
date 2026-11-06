"""Accidental-I/O guard for approved code, NOT a Python security sandbox.

An OS low-privilege service is still required for the LP profile. Audited Python
operations cannot prevent malicious native code or a privileged device owner.
"""
from __future__ import annotations

from contextlib import contextmanager
import os
import sys
import threading

from .limits import Rejected

_local = threading.local()
_installed = False


@contextmanager
def ciphertext_write():
    old = getattr(_local, 'ciphertext', False)
    _local.ciphertext = True
    try:
        yield
    finally:
        _local.ciphertext = old


@contextmanager
def loop_bootstrap():
    # Only asyncio's own loop construction; never surround import/graph/node
    # execution with this allowance. Windows socketpair may use loopback here.
    old = getattr(_local, 'loop', False)
    _local.loop = True
    try:
        yield
    finally:
        _local.loop = old


def _audit(event, args):
    if event.startswith('socket.') and not getattr(_local, 'loop', False):
        raise Rejected('ENVIRONMENT')
    if event in {'subprocess.Popen', 'os.system', 'os.posix_spawn', 'os.posix_spawnp',
                 'os.exec', 'os.fork', 'os.forkpty', 'os.startfile'}:
        raise Rejected('ENVIRONMENT')
    if event in {'os.mkdir', 'os.remove', 'os.rename', 'os.rmdir', 'os.link',
                 'os.symlink', 'os.truncate', 'os.chmod', 'os.chown'}:
        if not getattr(_local, 'ciphertext', False):
            raise Rejected('ENVIRONMENT')
    if event == 'open':
        _, mode, flags = args
        writes = isinstance(mode, str) and any(ch in mode for ch in 'wax+')
        writes = writes or bool(flags & (os.O_WRONLY | os.O_RDWR | os.O_CREAT |
                                        os.O_TRUNC | os.O_APPEND))
        if writes and not getattr(_local, 'ciphertext', False):
            raise Rejected('ENVIRONMENT')


def install():
    global _installed
    if _installed:
        raise Rejected('ENVIRONMENT')
    sys.addaudithook(_audit)
    _installed = True
