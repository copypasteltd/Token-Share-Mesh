"""Serial original-session Comfy executor, private pipes and real stop facts.

This is an internal engine, not an SDK server or a grant verifier. Only the
original host may launch it after independent approval and physical admission.
"""
from __future__ import annotations

import base64
from dataclasses import dataclass
import gc
import hashlib
import os
import threading
import time

from . import PROFILE, RELEASE, UPSTREAM_COMMIT
from .environment import Environment
from .io_guard import install
from .limits import CHUNK, MAX_MEDIA, Rejected, canonical, digest, fields
from .limits import identifier, integer, strict_json
from . import private_nodes, wire
from .spool import scope_bytes
from .workspace import Workspace


def decode_key(value):
    if type(value) is not str or len(value) != 43:
        raise Rejected('FORMAT')
    try:
        decoded = base64.b64decode(value + '=', altchars=b'-_', validate=True)
    except Exception:
        raise Rejected('FORMAT') from None
    if len(decoded) != 32 or base64.urlsafe_b64encode(decoded).rstrip(b'=').decode('ascii') != value:
        raise Rejected('FORMAT')
    return decoded


class Control:
    def __init__(self, session, generation, hard_ms):
        self.session, self.generation = session, generation
        self.lock = threading.RLock()
        self.changed = threading.Event()
        self.closed = False
        self.active = None
        self.cancelled_ids = set()
        self.seen = set()
        self.deadline = None
        self.session_deadline = time.monotonic() + max(0, hard_ms / 1000 - time.time())
        self.interrupt = None
        self.authorized = False
        self.grant_digest = None
        self.hard_ms = hard_ms

    def authorize(self, grant_digest, hard_ms):
        # The original host has independently verified its current signed grant
        # and fresh physical/control owner. This private command is NOT a JWT
        # verifier, payment fact, renewed term, remote readiness or cleanup proof.
        digest(grant_digest)
        integer(hard_ms, 1, 1 << 53)
        with self.lock:
            remaining = hard_ms / 1000 - time.time()
            if (self.closed or self.authorized or self.active is not None or
                    time.monotonic() >= self.session_deadline or not 0 < remaining <= 86400):
                raise Rejected('CANCELLED')
            self.authorized = True
            self.grant_digest = grant_digest
            self.hard_ms = hard_ms
            self.session_deadline = time.monotonic() + remaining
            self.changed.set()

    def cancelled(self):
        with self.lock:
            return (self.closed or time.monotonic() >= self.session_deadline or self.active in self.cancelled_ids or
                    self.deadline is not None and time.monotonic() >= self.deadline)

    def bind_interrupt(self, callback):
        if not callable(callback):
            raise Rejected('ENVIRONMENT')
        with self.lock:
            if self.interrupt is not None:
                raise Rejected('UNKNOWN')
            self.interrupt = callback
            already_cancelled = self.cancelled()
        if already_cancelled:
            callback(True)

    def start(self, invocation, cutoff_ms):
        with self.lock:
            if self.closed or not self.authorized or self.active is not None or invocation in self.seen or len(self.seen) >= 1024:
                raise Rejected('UNKNOWN')
            remaining = cutoff_ms / 1000 - time.time()
            if remaining <= 0 or remaining > 86400 or cutoff_ms > self.hard_ms or invocation in self.cancelled_ids or time.monotonic() >= self.session_deadline:
                raise Rejected('CANCELLED')
            self.seen.add(invocation)
            self.active = invocation
            self.deadline = min(time.monotonic() + remaining, self.session_deadline)
            if self.cancelled():
                raise Rejected('CANCELLED')
            self.changed.set()

    def finish(self):
        with self.lock:
            if self.active is not None:
                self.cancelled_ids.add(self.active)
            self.active = None
            self.deadline = None
            self.changed.set()

    def stop(self, invocation=None):
        with self.lock:
            if invocation is None:
                self.closed = True
            else:
                if len(self.cancelled_ids) >= 1024 and invocation not in self.cancelled_ids:
                    self.closed = True
                else:
                    self.cancelled_ids.add(invocation)
                if invocation == self.active:
                    self.deadline = None
            interrupt = self.interrupt if invocation is None or invocation == self.active else None
            self.changed.set()
        if interrupt is not None:
            try:
                interrupt(True)
            except BaseException:
                with self.lock:
                    self.closed = True

    def watch(self):
        while True:
            self.changed.clear()
            with self.lock:
                if self.closed:
                    return
                deadline, invocation = self.deadline, self.active
                target = min(self.session_deadline, deadline) if deadline is not None else self.session_deadline
            if not self.changed.wait(max(0, target - time.monotonic())):
                if time.monotonic() >= self.session_deadline:
                    self.stop()
                    return
                if invocation is not None:
                    self.stop(invocation)

    def receive(self, source, output=None):
        try:
            while True:
                frame = wire.read_frame(source, 4097)
                if frame is None:
                    self.stop()
                    return
                kind, raw = frame
                value = strict_json(raw, 4096)
                if kind == wire.AUTHORIZE:
                    fields(value, {'session_id', 'local_generation', 'grant_digest', 'hard_deadline_unix_ms'})
                    if value['session_id'] != self.session or value['local_generation'] != self.generation:
                        raise Rejected()
                    self.authorize(value['grant_digest'], value['hard_deadline_unix_ms'])
                    if output is not None:
                        output.send(wire.STATE, {'phase': 'AUTHORIZED', 'session_id': self.session,
                            'local_generation': self.generation, 'grant_digest': self.grant_digest})
                    continue
                if kind == wire.STOP_SESSION:
                    fields(value, {'session_id', 'local_generation'})
                    if value['session_id'] != self.session or value['local_generation'] != self.generation:
                        raise Rejected()
                    self.stop()
                    return
                if kind != wire.CANCEL:
                    raise Rejected()
                fields(value, {'session_id', 'local_generation', 'invocation_id'})
                if value['session_id'] != self.session or value['local_generation'] != self.generation:
                    raise Rejected()
                self.stop(identifier(value['invocation_id']))
        except BaseException:
            self.stop()


class MetadataServer:
    client_id = None
    last_node_id = None
    sockets_metadata = {}

    def send_sync(self, event, data, sid=None):
        # Comfy events may contain input tensors, graph/history and exception
        # strings. None are forwarded, retained or logged by this server.
        return None


@dataclass(repr=False)
class Job:
    scope: dict
    workspace: Workspace
    recipe: object
    assets: dict
    cancelled: object
    output: object = None


def _executor(environment):
    import execution

    class PrivateExecutor(execution.PromptExecutor):
        def add_message(self, event, data, broadcast=False):
            return None

        def handle_execution_error(self, prompt_id, prompt, current_outputs, executed, error, ex):
            self.success = False

    return PrivateExecutor(MetadataServer(), cache_type=execution.CacheType.NONE,
                           cache_args={'ram': 0, 'ram_inactive': 0})


def _assets(source, declarations, workspace, scope, cancelled):
    if type(declarations) is not list or len(declarations) > 4:
        raise Rejected('LIMIT')
    total, result = 0, {}
    for declaration in declarations:
        fields(declaration, {'asset_id', 'plaintext_bytes', 'plaintext_sha256'})
        asset_id = identifier(declaration['asset_id'])
        length = integer(declaration['plaintext_bytes'], 1, MAX_MEDIA)
        expected = digest(declaration['plaintext_sha256'])
        total += length
        if asset_id in result or total > MAX_MEDIA:
            raise Rejected('LIMIT')
        begin = wire.read_json(source, wire.ASSET_BEGIN, 4096)
        if begin != {'asset_id': asset_id}:
            raise Rejected('FORMAT')
        spool = workspace.spool(scope, length)
        h = hashlib.sha256()
        received = 0
        while received < length:
            if cancelled():
                raise Rejected('CANCELLED')
            frame = wire.read_frame(source, CHUNK + 1)
            if frame is None or frame[0] != wire.ASSET_DATA or not 0 < len(frame[1]) <= length - received:
                raise Rejected('FORMAT')
            spool.write(frame[1])
            h.update(frame[1])
            received += len(frame[1])
            # Do not retain the last plaintext input chunk while computing.
            frame = None
        end = wire.read_json(source, wire.ASSET_END, 4096)
        if end != {'asset_id': asset_id} or h.hexdigest() != expected:
            raise Rejected('INTEGRITY')
        spool.seal()
        result[asset_id] = spool
    return result


def invoke(source, output, environment, control, session, value):
    fields(value, {'scope', 'hard_deadline_unix_ms', 'workspace_root',
                   'workspace_anchor_sha256', 'workspace_journal_key',
                   'parameters', 'assets'})
    scope = value['scope']
    scope_bytes(scope)
    if scope['session_id'] != session['session_id'] or scope['local_generation'] != session['local_generation'] or scope['environment_digest'] != session['environment_digest'] or scope['recipe_digest'] != session['recipe_digest']:
        raise Rejected()
    invocation = scope['invocation_id']
    cutoff = integer(value['hard_deadline_unix_ms'], 1, control.hard_ms)
    control.start(invocation, cutoff)
    workspace, job, executor, graph = None, None, None, None
    receipt = None
    success, transferred, synchronized, assets_complete = False, False, False, False
    try:
        # Validate all public scalar/asset bindings before creating any payload
        # or entering an engine. This is not platform INVOKE authority.
        declarations = value['assets']
        if type(declarations) is not list or len(declarations) > 4:
            raise Rejected('LIMIT')
        asset_ids = {identifier(a['asset_id']) for a in declarations if type(a) is dict and 'asset_id' in a}
        graph = environment.recipe.bind(value['parameters'], asset_ids)
        workspace = Workspace(value['workspace_root'], value['workspace_anchor_sha256'],
                              decode_key(value['workspace_journal_key']), scope, control.cancelled)
        assets = _assets(source, declarations, workspace, scope, control.cancelled)
        assets_complete = True
        value['workspace_journal_key'] = ''
        if control.cancelled():
            raise Rejected('CANCELLED')
        output.send(wire.ACCEPTED, {'invocation_id': invocation, 'phase': 'EXECUTING'})
        job = Job(scope, workspace, environment.recipe, assets, control.cancelled)
        private_nodes.enter(job)
        executor = _executor(environment)
        import execution
        validation = environment.runner.run(execution.validate_prompt(invocation, graph))
        if not validation[0] or set(validation[2]) != {environment.recipe.output} or control.cancelled():
            raise Rejected('RECIPE')
        environment.runner.run(executor.execute_async(graph, invocation, extra_data={},
                                                      execute_outputs=[environment.recipe.output]))
        environment.synchronize()
        synchronized = True
        if not executor.success or job.output is None or control.cancelled():
            raise Rejected('CANCELLED' if control.cancelled() else 'ENCODER')
        receipt = job.output.receipt()
        receipt['key'] = base64.urlsafe_b64encode(receipt['key']).rstrip(b'=').decode('ascii')
        receipt['media_kind'] = environment.recipe.kind
        receipt['invocation_id'] = invocation
        # This private result receipt is NOT a buyer ACK, metering proof or
        # platform cleanup/payment release; the host must own its key custody.
        output.send(wire.RESULT, receipt)
        transferred, success = True, True
    except BaseException as error:
        code = error.code if isinstance(error, Rejected) else 'UNKNOWN'
        output.send(wire.ERROR, {'invocation_id': invocation, 'code': code})
    finally:
        # Python strings/native library copies cannot promise forensic wiping.
        # Drop owned input/key references before synchronization/idle waiting.
        if receipt is not None:
            receipt['key'] = ''
            receipt.clear()
        value['workspace_journal_key'] = ''
        value['parameters'] = {}
        if not assets_complete:
            # A partially received content stream has no safe next-message
            # boundary. Close this original session instead of guessing it.
            control.stop()
        control.stop(invocation)
        private_nodes.leave()
        if graph is not None:
            graph.clear()
        if executor is not None:
            executor.reset()
            executor.history_result = {}
        if job is not None:
            job.assets.clear()
            job.output = None
        environment.drop_call_state()
        gc.collect()
        # Synchronization is after graph/cache teardown as well as compute.
        # Any exception here leaves UNKNOWN; no STOPPED frame is manufactured.
        environment.synchronize()
        synchronized = True
        if workspace is not None:
            workspace.close()
        control.finish()
        if synchronized:
            output.send(wire.STOPPED, {'invocation_id': invocation,
                'work_stopped': True, 'future_start_revoked': True,
                'input_keys_cleared': True,
                'output_keys': 'PRIVATE_HOST_TRANSFERRED' if transferred else 'REVOKED',
                'result_succeeded': success})


def run(source, control_source, protocol_output):
    output = wire.Writer(protocol_output)
    environment, control = None, None
    code = 1
    try:
        initial = wire.read_json(source, wire.INIT, 32 << 20)
        fields(initial, {'session', 'environment'})
        session = initial['session']
        fields(session, {'session_id', 'resource_id', 'local_generation',
                         'environment_digest', 'recipe_digest', 'hard_deadline_unix_ms'})
        identifier(session['session_id'])
        identifier(session['resource_id'])
        digest(session['environment_digest'])
        digest(session['recipe_digest'])
        scope_bytes({'session_id': session['session_id'], 'invocation_id': 'initial_scope',
                     'local_generation': session['local_generation'],
                     'environment_digest': session['environment_digest'],
                     'recipe_digest': session['recipe_digest']})
        if initial['environment']['environment_digest'] != session['environment_digest'] or initial['environment']['recipe_sha256'] != session['recipe_digest']:
            raise Rejected()
        hard = integer(session['hard_deadline_unix_ms'], 1, 1 << 53)
        if not time.time() * 1000 < hard <= (time.time() + 86400) * 1000:
            raise Rejected('CANCELLED')
        control = Control(session['session_id'], session['local_generation'], hard)
        reader = threading.Thread(target=control.receive, args=(control_source, output), daemon=True)
        reader.start()
        deadline = threading.Thread(target=control.watch, daemon=True)
        deadline.start()
        install()
        environment = Environment(initial['environment'], control.cancelled)
        initial.clear()
        initial = None
        control.bind_interrupt(environment.management.interrupt_current_processing)
        source = wire.CancelledReader(source, control.cancelled)
        if control.cancelled():
            raise Rejected('CANCELLED')
        output.send(wire.READY, {'profile_id': PROFILE, 'engine_release': RELEASE,
            'upstream_commit': UPSTREAM_COMMIT, 'session_id': session['session_id'],
            'environment_digest': session['environment_digest'],
            'recipe_digest': session['recipe_digest'], 'qualification': 'NOT_GRANTED'})
        while not control.closed:
            try:
                frame = wire.read_frame(source)
            except Rejected:
                if control.closed:
                    break
                raise
            if frame is None:
                control.stop()
                break
            kind, raw = frame
            if kind == wire.SHUTDOWN:
                value = strict_json(raw, 4096)
                raw = None
                frame = None
                if value != {'session_id': session['session_id'], 'local_generation': session['local_generation']}:
                    raise Rejected()
                control.stop()
                break
            if kind != wire.INVOKE:
                raise Rejected()
            value = strict_json(raw)
            raw = None
            frame = None
            try:
                invoke(source, output, environment, control, session, value)
            finally:
                value.clear()
                value = None
        environment.close()
        environment = None
        output.send(wire.CLOSED, {'session_id': session['session_id'],
            'work_stopped': True, 'future_starts_revoked': True,
            'requires_original_host_process_and_pipe_join': True})
        code = 0
    except BaseException as error:
        if control is not None:
            control.stop()
        try:
            output.send(wire.ERROR, {'code': error.code if isinstance(error, Rejected) else 'UNKNOWN'})
        except BaseException:
            pass
    finally:
        if control is not None:
            control.stop()
        if environment is not None:
            try:
                environment.close()
            except BaseException:
                pass
    return code
