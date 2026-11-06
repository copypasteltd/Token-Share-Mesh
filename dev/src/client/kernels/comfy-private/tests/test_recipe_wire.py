"""SOURCE_ONLY definitions. No import of Torch, Pillow, PyAV or ComfyUI."""
import copy
import io
import struct
import unittest

from test_support import scope
from mesh_comfy.io_guard import _audit, ciphertext_write
from mesh_comfy.limits import Rejected, canonical, strict_json
from mesh_comfy.recipe import Recipe
from mesh_comfy import wire


def recipe_data():
    return {'profile_id': 'P11_COMFY_PRIVATE_RECIPE_1', 'media_kind': 'PNG',
            'graph': {'text': {'class_type': 'ApprovedText', 'inputs': {'text': ''}},
                      'sink': {'class_type': 'MeshPrivatePNG', 'inputs': {'images': ['text', 0]}}},
            'output_node_id': 'sink', 'maximum_output_bytes': 4096,
            'parameters': [{'name': 'prompt', 'node_id': 'text', 'input_name': 'text',
                            'value_type': 'STRING', 'required': True, 'maximum_utf8_bytes': 16,
                            'minimum': 0, 'maximum': 1, 'choices': []}]}


def recipe(value=None):
    return Recipe(canonical(recipe_data() if value is None else value), {'ApprovedText', 'MeshPrivatePNG'})


class RecipeWireBoundary(unittest.TestCase):
    def test_parameters_bind_only_approved_slots_and_do_not_mutate_next_call(self):
        approved = recipe()
        first = approved.bind({'prompt': '私有提示词'}, set())
        second = approved.bind({'prompt': 'next'}, set())
        self.assertEqual(first['text']['inputs']['text'], '私有提示词')
        self.assertEqual(second['text']['inputs']['text'], 'next')
        self.assertEqual(approved._graph['text']['inputs']['text'], '')
        for bad in ({}, {'prompt': 'x' * 17}, {'prompt': 'x\0y'}, {'prompt': 'x', 'graph': {}}, {'prompt': []}):
            with self.assertRaises(Rejected):approved.bind(bad, set())

    def test_native_savers_unknown_nodes_and_second_sink_are_refused(self):
        for change in ('native', 'unknown', 'second', 'metadata'):
            value = recipe_data()
            if change == 'native':value['graph']['sink']['class_type'] = 'SaveImage'
            if change == 'unknown':value['graph']['text']['class_type'] = 'UnapprovedNode'
            if change == 'second':value['graph']['other_sink'] = copy.deepcopy(value['graph']['sink'])
            if change == 'metadata':value['graph']['text']['_meta'] = {'title': 'private'}
            with self.assertRaises(Rejected):recipe(value)

    def test_path_model_and_duplicate_parameter_targets_are_immutable(self):
        for change in ('path', 'clip_name', 'duplicate'):
            value = recipe_data()
            if change == 'duplicate':
                second = copy.deepcopy(value['parameters'][0]);second['name'] = 'another';value['parameters'].append(second)
            else:
                value['graph']['text']['inputs'][change] = 'original'
                value['parameters'][0]['input_name'] = change
            with self.assertRaises(Rejected):recipe(value)

    def test_strict_json_rejects_duplicate_keys_nan_surrogates_and_utf8(self):
        for raw in (b'{"prompt":1,"prompt":2}', b'{"a":NaN}', b'{"a":1e400}', b'{"a":"\\ud800"}', b'{"a":"\xff"}'):
            with self.assertRaises(Rejected):strict_json(raw)

    def test_private_wire_partial_reads_truncation_and_preallocation_limit(self):
        raw = canonical({'session_id': scope()['session_id']})
        encoded = struct.pack('<IB', len(raw) + 1, wire.SHUTDOWN) + raw

        class Partial(io.BytesIO):
            def readinto(self, data):return super().readinto(data[:min(2, len(data))])

        self.assertEqual(wire.read_json(Partial(encoded), wire.SHUTDOWN), strict_json(raw))
        with self.assertRaises(Rejected):wire.read_frame(io.BytesIO(encoded[:-1]))
        for size in (0, 0xffffffff):
            source = io.BytesIO(struct.pack('<I', size) + b'unread-private-payload')
            with self.assertRaises(Rejected):wire.read_frame(source)
            self.assertEqual(source.tell(), 4)

    def test_private_output_writer_accepts_only_bounded_worker_metadata(self):
        output = io.BytesIO()
        writer = wire.Writer(output)
        writer.send(wire.STATE, {'phase': 'EXECUTING'})
        self.assertEqual(wire.read_json(io.BytesIO(output.getvalue()), wire.STATE), {'phase': 'EXECUTING'})
        with self.assertRaises(Rejected):writer.send(wire.INVOKE, {'prompt': 'private'})
        with self.assertRaises(Rejected):writer.send(wire.RESULT, {'private': 'x' * 65537})

    def test_accidental_io_guard_denies_network_subprocess_and_plain_file_writes(self):
        for event, args in [('socket.connect', ()), ('subprocess.Popen', ()), ('os.system', ()), ('open', ('private.png', 'wb', 0)), ('os.rename', ('a', 'b', 0, 0))]:
            with self.assertRaises(Rejected):_audit(event, args)
        _audit('open', ('approved.safetensors', 'rb', 0))
        with ciphertext_write():_audit('open', ('owned-cipher.bin', 'wb', 0))
        with self.assertRaises(Rejected):_audit('open', ('private.png', 'wb', 0))

    def test_cancelled_owned_pipe_returns_before_waiting_for_content(self):
        import os
        read_fd, write_fd = os.pipe()
        with os.fdopen(read_fd, 'rb', buffering=0) as source, os.fdopen(write_fd, 'wb', buffering=0):
            reader = wire.CancelledReader(source, lambda: True)
            with self.assertRaises(Rejected):reader.readinto(bytearray(8))
