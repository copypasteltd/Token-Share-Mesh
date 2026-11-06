"""SOURCE_ONLY original cancellation/deadline definitions, no engine launched."""
import io
import struct
import time
import unittest
from unittest.mock import patch

from test_support import scope
from mesh_comfy.limits import Rejected, canonical
from mesh_comfy.worker import Control
from mesh_comfy import wire


class ControlBoundary(unittest.TestCase):
    def control(self):
        return Control('session_original', '19', int((time.time() + 60) * 1000))

    def test_cancel_before_start_is_a_permanent_original_tombstone(self):
        control = self.control();control.stop('invoke_original')
        with self.assertRaises(Rejected):control.start('invoke_original', int((time.time() + 30) * 1000))
        self.assertIn('invoke_original', control.cancelled_ids)
        self.assertIsNone(control.active)

    def test_finish_never_allows_reentering_the_original_invocation(self):
        control = self.control();control.start('invoke_original', int((time.time() + 30) * 1000))
        control.finish()
        with self.assertRaises(Rejected):control.start('invoke_original', int((time.time() + 30) * 1000))
        control.start('invoke_second', int((time.time() + 30) * 1000))
        control.finish();control.stop()

    def test_foreign_child_cancel_does_not_interrupt_current_original_work(self):
        control = self.control();observed = [];control.bind_interrupt(observed.append)
        control.start('invoke_original', int((time.time() + 30) * 1000))
        control.stop('invoke_other');self.assertEqual(observed, []);self.assertFalse(control.cancelled())
        control.stop('invoke_original');self.assertEqual(observed, [True]);self.assertTrue(control.cancelled())

    def test_native_interrupt_binding_preserves_already_cancelled_session(self):
        control = self.control();control.stop();observed = []
        control.bind_interrupt(observed.append)
        self.assertEqual(observed, [True])
        with self.assertRaises(Rejected):control.bind_interrupt(observed.append)

    def test_monotonic_child_and_session_deadlines_survive_wall_clock_rollback(self):
        with patch('mesh_comfy.worker.time.time', return_value=1000), patch('mesh_comfy.worker.time.monotonic', return_value=50):
            control = Control('session_original', '19', 1060000)
            control.start('invoke_original', 1010000)
        with patch('mesh_comfy.worker.time.time', return_value=900), patch('mesh_comfy.worker.time.monotonic', return_value=61):
            self.assertTrue(control.cancelled())
        control.finish()
        with patch('mesh_comfy.worker.time.monotonic', return_value=111):self.assertTrue(control.cancelled())

    def test_foreign_control_identity_or_lost_control_pipe_stops_original_session(self):
        for value in ({'session_id': 'session_other', 'local_generation': '19'}, {'session_id': 'session_original', 'local_generation': '20'}):
            control = self.control();raw = canonical(value)
            control.receive(io.BytesIO(struct.pack('<IB', len(raw) + 1, wire.STOP_SESSION) + raw))
            self.assertTrue(control.closed)
        control = self.control();control.receive(io.BytesIO());self.assertTrue(control.closed)
