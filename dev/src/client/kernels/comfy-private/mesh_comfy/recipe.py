"""Immutable owner-approved graph, with finite typed scalar/asset slots."""
from __future__ import annotations

import copy
import math
import re

from .limits import Rejected, fields, integer, identifier, strict_json

PROFILE = 'P11_COMFY_PRIVATE_RECIPE_1'
PRIVATE_SINKS = {'PNG': 'MeshPrivatePNG', 'MP4': 'MeshPrivateMP4',
                 'WAV': 'MeshPrivateWAV', 'GLB': 'MeshPrivateGLB'}
DENIED_CLASSES = {'SaveImage', 'PreviewImage', 'SaveVideo', 'SaveAnimatedWEBP',
                  'SaveAnimatedPNG', 'SaveAudio', 'SaveAudioMP3', 'SaveAudioOpus',
                  'SaveGLB', 'SaveOBJ', 'LoadImage', 'LoadImageMask', 'LoadAudio',
                  'LoadVideo', 'VHS_LoadVideo', 'VHS_VideoCombine'}
DENIED_SLOTS = {'filename', 'filename_prefix', 'path', 'file', 'url', 'ckpt_name',
                'lora_name', 'vae_name', 'model_name', 'device', 'class_type',
                'clip_name', 'clip_name1', 'clip_name2', 'clip_name3', 'clip_name4',
                'unet_name', 'control_net_name', 'style_model_name', 'upscale_model',
                'audio_encoder', 'model_path', 'cache_dir', 'output_dir', 'input_dir'}
NODE_ID = re.compile(r'^[A-Za-z0-9_-]{1,64}$')


class Recipe:
    def __init__(self, raw, allowed_classes):
        value = strict_json(raw)
        fields(value, {'profile_id', 'media_kind', 'graph', 'output_node_id',
                       'parameters', 'maximum_output_bytes'})
        if value['profile_id'] != PROFILE or value['media_kind'] not in PRIVATE_SINKS:
            raise Rejected('RECIPE')
        self.kind = value['media_kind']
        self.maximum = integer(value['maximum_output_bytes'], 1024, 64 << 20)
        graph = value['graph']
        if type(graph) is not dict or not 1 <= len(graph) <= 128:
            raise Rejected('RECIPE')
        sinks = []
        for node_id, node in graph.items():
            if type(node_id) is not str or NODE_ID.fullmatch(node_id) is None:
                raise Rejected('RECIPE')
            fields(node, {'class_type', 'inputs'})
            cls = node['class_type']
            if type(cls) is not str or cls not in allowed_classes or cls in DENIED_CLASSES:
                raise Rejected('RECIPE')
            if cls in PRIVATE_SINKS.values():
                sinks.append(node_id)
            if type(node['inputs']) is not dict or len(node['inputs']) > 64:
                raise Rejected('RECIPE')
        if sinks != [value['output_node_id']] or graph[sinks[0]]['class_type'] != PRIVATE_SINKS[self.kind]:
            raise Rejected('RECIPE')
        self.output = sinks[0]
        params = value['parameters']
        if type(params) is not list or len(params) > 32:
            raise Rejected('RECIPE')
        self.slots, targets = {}, set()
        for slot in params:
            fields(slot, {'name', 'node_id', 'input_name', 'value_type', 'required',
                          'maximum_utf8_bytes', 'minimum', 'maximum', 'choices'})
            name = identifier(slot['name'])
            if type(slot['node_id']) is not str or type(slot['input_name']) is not str:
                raise Rejected('RECIPE')
            target = (slot['node_id'], slot['input_name'])
            if name in self.slots or target in targets or slot['node_id'] not in graph:
                raise Rejected('RECIPE')
            if slot['input_name'] in DENIED_SLOTS or slot['input_name'] not in graph[slot['node_id']]['inputs']:
                raise Rejected('RECIPE')
            if type(slot['required']) is not bool or slot['value_type'] not in {'STRING', 'INTEGER', 'NUMBER', 'ENUM', 'ASSET'}:
                raise Rejected('RECIPE')
            if slot['value_type'] == 'ASSET' and (graph[slot['node_id']]['class_type'] != 'MeshPrivateImage' or slot['input_name'] != 'asset_id'):
                raise Rejected('RECIPE')
            if slot['input_name'] == 'asset_id' and slot['value_type'] != 'ASSET':
                raise Rejected('RECIPE')
            integer(slot['maximum_utf8_bytes'], 1, 65536)
            if type(slot['minimum']) not in (int, float) or type(slot['maximum']) not in (int, float):
                raise Rejected('RECIPE')
            if not math.isfinite(slot['minimum']) or not math.isfinite(slot['maximum']) or slot['minimum'] > slot['maximum']:
                raise Rejected('RECIPE')
            if type(slot['choices']) is not list or len(slot['choices']) > 64 or any(type(c) is not str or len(c.encode('utf-8')) > 256 for c in slot['choices']):
                raise Rejected('RECIPE')
            self.slots[name] = slot
            targets.add(target)
        self._graph = graph

    def bind(self, parameters, asset_ids):
        if type(parameters) is not dict or set(parameters) - set(self.slots):
            raise Rejected('RECIPE')
        graph = copy.deepcopy(self._graph)
        for name, slot in self.slots.items():
            if name not in parameters:
                if slot['required']:
                    raise Rejected('RECIPE')
                continue
            value = parameters[name]
            kind = slot['value_type']
            if kind in {'STRING', 'ENUM', 'ASSET'}:
                if type(value) is not str or '\0' in value or len(value.encode('utf-8')) > slot['maximum_utf8_bytes']:
                    raise Rejected('RECIPE')
                if kind == 'ENUM' and value not in slot['choices']:
                    raise Rejected('RECIPE')
                if kind == 'ASSET' and value not in asset_ids:
                    raise Rejected('RECIPE')
            elif kind == 'INTEGER':
                integer(value, slot['minimum'], slot['maximum'])
            elif type(value) not in (int, float) or not math.isfinite(value) or not slot['minimum'] <= value <= slot['maximum']:
                raise Rejected('RECIPE')
            graph[slot['node_id']]['inputs'][slot['input_name']] = value
        return graph

    def clear(self):
        # The immutable graph contains only approved defaults. Runtime graph
        # copies and user scalar strings are released after each invocation.
        self._graph.clear()
