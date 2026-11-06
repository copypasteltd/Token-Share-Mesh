"""SOURCE_ONLY: actual crypto/file boundary definitions, not yet executed."""
from contextlib import contextmanager
import hashlib
import io
import os
from pathlib import Path
import struct
import unittest

from test_support import owned_directory, scope
from mesh_comfy.limits import Rejected
from mesh_comfy.spool import CipherSpool, scope_bytes
from mesh_comfy.workspace import Workspace


class CipherBoundary(unittest.TestCase):
    @contextmanager
    def directory(self):
        # Release every entered file/key owner before Windows removes the
        # original temporary directory, including assertion-failure paths.
        with owned_directory() as directory:
            try:
                yield directory
            finally:
                self.doCleanups()

    def make_spool(self, directory, limit, cancelled=lambda: False):
        reservations = []

        def reserve(name):
            self.assertFalse((Path(directory) / name).exists())
            reservations.append(name)

        spool = CipherSpool(directory, scope(), limit, reserve, cancelled)
        self.addCleanup(spool.close)
        self.assertEqual(len(reservations), 1)
        return spool

    def test_original_scope_roundtrip_cross_record_seek_and_no_plaintext_file(self):
        with self.directory() as directory:
            data = b'PRIVATE-PROMPT-AND-PIXELS|' * 6000
            spool = self.make_spool(directory, len(data))
            for offset in range(0, len(data), 777):
                self.assertEqual(spool.write(data[offset:offset + 777]), len(data[offset:offset + 777]))
            spool.seal()
            receipt = spool.receipt()
            disk = (Path(directory) / receipt['basename']).read_bytes()
            self.assertNotIn(b'PRIVATE-PROMPT-AND-PIXELS|', disk)
            self.assertEqual(len(disk), 48 + len(data) + receipt['data_records'] * 25 + 73)
            with spool.reader() as reader:
                for offset in (0, 65530, 65536, 131069, len(data) - 3):
                    reader.seek(offset)
                    self.assertEqual(reader.read(83), data[offset:offset + 83])
            # Sequential consumers may reopen; two simultaneous readers may not.
            with spool.reader() as reader:
                self.assertEqual(reader.read(7), data[:7])
            spool.close()
            self.assertTrue((Path(directory) / receipt['basename']).exists())

    def test_independent_aead_decrypts_end_and_first_data_record(self):
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
        with self.directory() as directory:
            spool = self.make_spool(directory, 100)
            spool.write(b'original');spool.seal()
            receipt = spool.receipt()
            disk = (Path(directory) / receipt['basename']).read_bytes()
            header = disk[:48]
            self.assertEqual(header[:8], b'P11CS01\0')
            self.assertEqual(header[16:], hashlib.sha256(scope_bytes(scope())).digest())
            aead = AESGCM(receipt['key'])
            record = disk[48:57]
            self.assertEqual(record, struct.pack('<IBI', 0, 1, 8))
            self.assertEqual(aead.decrypt(header[8:16] + struct.pack('<I', 0), disk[57:81], header + record), b'original')
            end_header = disk[-73:-64]
            self.assertEqual(end_header, struct.pack('<IBI', 1, 2, 48))
            end = aead.decrypt(header[8:16] + struct.pack('<I', 1), disk[-64:], header + end_header)
            self.assertEqual(end, struct.pack('<QQ32s', 1, 8, hashlib.sha256(b'original').digest()))

    def test_reservation_failure_creates_no_payload(self):
        with self.directory() as directory:
            def refuse(_):
                raise Rejected('UNKNOWN')
            with self.assertRaises(Rejected):
                CipherSpool(directory, scope(), 10, refuse, lambda: False)
            self.assertEqual(list(Path(directory).iterdir()), [])

    def test_tampered_data_record_rejected_before_exposing_plaintext(self):
        with self.directory() as directory:
            spool = self.make_spool(directory, 20)
            spool.write(b'private');spool.seal()
            path = Path(directory) / spool.receipt()['basename']
            with path.open('r+b', buffering=0) as file:
                file.seek(58);value = file.read(1);file.seek(58);file.write(bytes([value[0] ^ 1]))
            with spool.reader() as reader:
                with self.assertRaises(Rejected):
                    reader.read(7)

    def test_missing_or_appended_authenticated_end_refuses_reader(self):
        for append in (False, True):
            with self.subTest(append=append), self.directory() as directory:
                spool = self.make_spool(directory, 20)
                spool.write(b'private');spool.seal()
                path = Path(directory) / spool.receipt()['basename']
                with path.open('r+b', buffering=0) as file:
                    file.seek(0, io.SEEK_END)
                    if append:
                        file.write(b'x')
                    else:
                        file.truncate(file.tell() - 1)
                with self.assertRaises(Rejected):
                    spool.reader()

    def test_one_reader_readall_and_out_of_range_seek_limits(self):
        with self.directory() as directory:
            spool = self.make_spool(directory, 10)
            spool.write(b'original');spool.seal()
            with spool.reader() as reader:
                with self.assertRaises(Rejected):spool.reader()
                with self.assertRaises(Rejected):reader.read()
                with self.assertRaises(Rejected):reader.read((1 << 20) + 1)
                with self.assertRaises(Rejected):reader.seek(-1)
                with self.assertRaises(Rejected):reader.seek(9)
                self.assertEqual(reader.read(0), b'')

    def test_limit_and_cancel_close_clear_owned_keys_without_deleting_cipher(self):
        with self.directory() as directory:
            stopped = [False]
            spool = self.make_spool(directory, 8, lambda: stopped[0])
            with self.assertRaises(Rejected):spool.write(b'x' * 9)
            spool.write(b'original');spool.seal()
            name = spool.receipt()['basename']
            reader = spool.reader();stopped[0] = True
            with self.assertRaises(Rejected):reader.read(1)
            spool.close()
            self.assertTrue(reader.closed)
            self.assertEqual(set(spool._key), {0})
            self.assertIsNone(spool._aead)
            self.assertTrue((Path(directory) / name).exists())

    def test_empty_spool_and_noncanonical_scope_cannot_produce_result(self):
        with self.directory() as directory:
            spool = self.make_spool(directory, 10)
            with self.assertRaises(Rejected):spool.seal()
        for generation in ('0', '019', '١٩', str(1 << 64)):
            value = scope();value['local_generation'] = generation
            with self.assertRaises(Rejected):scope_bytes(value)

    def test_workspace_anchor_and_encrypted_reservation_precede_payload(self):
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
        with self.directory() as directory:
            path = Path(directory)
            anchor = bytes(range(32));key = b'k' * 32
            (path / 'ownership.anchor').write_bytes(anchor)
            workspace = Workspace(directory, hashlib.sha256(anchor).hexdigest(), key, scope(), lambda: False)
            self.addCleanup(workspace.close)
            spool = workspace.spool(scope(), 20)
            spool.write(b'original');spool.seal()
            name = spool.receipt()['basename']
            raw = (path / (name + '.reservation')).read_bytes()
            self.assertNotIn(name.encode(), raw)
            record = AESGCM(key).decrypt(raw[:12], raw[12:], b'P11_COMFY_WORKSPACE_RESERVATION_1\n' + scope_bytes(scope()))
            self.assertIn(name.encode(), record)
            with self.assertRaises(Rejected):workspace.reserve(name)
            with self.assertRaises(Rejected):workspace.reserve('cipher-' + '../' * 10 + 'x.bin')
            workspace.close()
            self.assertTrue(spool.closed)
            self.assertTrue((path / name).exists())

    def test_foreign_workspace_anchor_or_scope_is_not_adopted(self):
        with self.directory() as directory:
            (Path(directory) / 'ownership.anchor').write_bytes(b'a' * 32)
            with self.assertRaises(Rejected):
                Workspace(directory, hashlib.sha256(b'b' * 32).hexdigest(), b'k' * 32, scope(), lambda: False)
            workspace = Workspace(directory, hashlib.sha256(b'a' * 32).hexdigest(), b'k' * 32, scope(), lambda: False)
            self.addCleanup(workspace.close)
            other = scope();other['invocation_id'] = 'invoke_other'
            with self.assertRaises(Rejected):workspace.spool(other, 10)
            self.assertEqual(len(list(Path(directory).iterdir())), 1)
