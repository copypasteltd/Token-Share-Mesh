package com.lma.app.mesh;

import android.app.Activity;
import android.app.Instrumentation;
import android.os.Bundle;
import com.lma.app.core.EngineWorkspace;
import com.lma.app.core.MeshWorkspaceOwnership;
import org.bouncycastle.crypto.params.Ed25519PrivateKeyParameters;
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters;
import org.bouncycastle.crypto.signers.Ed25519Signer;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.*;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.SocketTimeoutException;
import java.security.KeyStore;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;
import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

/** Physical-device boundary tests. Never auto-started by the application or the source preparation tool. */
public final class MeshBoundaryInstrumentation extends Instrumentation {
    private interface Case { void run(File scratch)throws Exception; }
    private File ownedRoot;
    @Override public void onCreate(Bundle arguments){super.onCreate(arguments);start();}
    @Override public void onStart(){
        Bundle result=new Bundle();JSONArray cases=new JSONArray();boolean success=false;
        try{
            if("1".equals(System.getProperty("ro.kernel.qemu"))||android.os.Build.FINGERPRINT.startsWith("generic"))throw new IOException("PHYSICAL_DEVICE_REQUIRED");
            ownedRoot=new File(getTargetContext().getNoBackupFilesDir(),"mesh-boundaries-"+UUID.randomUUID());Files.createDirectory(ownedRoot.toPath());
            run(cases,"AIB-001",this::identity);
            run(cases,"AIB-002",this::config);
            run(cases,"AIB-003",this::journal);
            run(cases,"AIB-004",this::renewal);
            run(cases,"AIB-005",this::workspace);
            run(cases,"AIB-006",this::resultCipher);
            run(cases,"AIB-007",this::unknownReport);
            success=true;
        }catch(Exception failure){result.putString("safe_failure",failure.getClass().getSimpleName());}
        finally{try{if(ownedRoot!=null)removeTestTree(ownedRoot);}catch(Exception cleanup){success=false;result.putString("safe_failure","TEST_CLEANUP_FAILED");}}
        result.putString("cases",cases.toString());result.putBoolean("commercial_qualification",false);result.putBoolean("model_executed",false);finish(success?Activity.RESULT_OK:Activity.RESULT_CANCELED,result);
    }
    private void run(JSONArray rows,String id,Case test)throws Exception{
        File scratch=new File(ownedRoot,id);Files.createDirectory(scratch.toPath());
        try{test.run(scratch);rows.put(new JSONObject().put("id",id).put("status","PASS"));}catch(Exception failure){rows.put(new JSONObject().put("id",id).put("status","FAIL"));throw failure;}
    }
    private static void require(boolean condition)throws IOException{if(!condition)throw new IOException("BOUNDARY_ASSERTION_FAILED");}
    private interface Failing { void run()throws Exception; }
    private static void rejected(Failing action)throws Exception{boolean failed=false;try{action.run();}catch(Exception expected){failed=true;}require(failed);}
    private void identity(File scratch)throws Exception{
        String original;byte[] message="synthetic control request".getBytes(StandardCharsets.US_ASCII);
        try(MeshDeviceIdentity owner=new MeshDeviceIdentity(scratch,true)){
            original=owner.publicKey();rejected(()->new MeshDeviceIdentity(scratch,false));String signature=owner.sign(message);
            Ed25519Signer verifier=new Ed25519Signer();verifier.init(false,new Ed25519PublicKeyParameters(MeshDeviceIdentity.decoded(original,32),0));verifier.update(message,0,message.length);require(verifier.verifySignature(MeshDeviceIdentity.decoded(signature,64)));
            byte[] changed=message.clone();changed[0]^=1;verifier.reset();verifier.update(changed,0,changed.length);require(!verifier.verifySignature(MeshDeviceIdentity.decoded(signature,64)));
        }
        try(MeshDeviceIdentity restored=new MeshDeviceIdentity(scratch,false)){require(original.equals(restored.publicKey()));}
    }
    private void config(File scratch)throws Exception{
        Fixture fixture=new Fixture();try(MeshDeviceIdentity identity=new MeshDeviceIdentity(scratch,true);MeshOwnerConfig store=new MeshOwnerConfig(scratch,true)){
            JSONObject owner=fixture.config(identity.publicKey());byte[] raw=owner.toString().getBytes(StandardCharsets.UTF_8);store.importApproved(raw,identity.publicKey());
            require(store.load(identity.publicKey()).getString("issuer_public_key").equals(owner.getString("issuer_public_key")));
            JSONObject changed=MeshJson.copy(owner);changed.put("unexpected",true);rejected(()->store.importApproved(changed.toString().getBytes(StandardCharsets.UTF_8),identity.publicKey()));
            changed.remove("unexpected");changed.put("device_public_key",fixture.pin());rejected(()->store.importApproved(changed.toString().getBytes(StandardCharsets.UTF_8),identity.publicKey()));
            rejected(()->MeshOwnerConfig.origin("http://example.com",true));rejected(()->MeshOwnerConfig.origin("http://127.0.0.1:9000",false));
        }
    }
    private void journal(File scratch)throws Exception{
        Fixture fixture=new Fixture();MeshLease lease=fixture.lease(1,20);JSONObject binding=fixture.binding();
        try(MeshJournal owner=new MeshJournal(scratch,binding,true)){
            rejected(()->owner.admit(lease,-1));MeshJournal.Work work=owner.admit(lease,1);rejected(()->owner.admit(lease,1));
            owner.executing(work);owner.stopping(work);rejected(()->owner.cleaned(work,fixture.hash,false,true,true));require(owner.held());
            owner.cleaned(work,fixture.hash,true,true,true);require(!owner.held());rejected(()->owner.admit(lease,1));
        }
        try(MeshJournal restored=new MeshJournal(scratch,binding,false)){require(!restored.held());require("CLEANED".equals(restored.receipt(lease.id).getString("phase")));}
        File other=new File(scratch,"interrupted");Files.createDirectory(other.toPath());
        try(MeshJournal interrupted=new MeshJournal(other,binding,true)){interrupted.admit(lease,1);}
        try(MeshJournal restored=new MeshJournal(other,binding,false)){require(restored.held());MeshJournal.Work original=restored.recoverable();require(original.generation==lease.generation);rejected(()->restored.admit(lease,2));}
    }
    private void renewal(File scratch)throws Exception{
        Fixture fixture=new Fixture();MeshLease lease=fixture.lease(1,20);
        try(MeshJournal journal=new MeshJournal(scratch,fixture.binding(),true)){
            MeshJournal.Work work=journal.admit(lease,1);journal.executing(work);byte[] next=fixture.wire(2,40);
            lease.renew(next,fixture.publicKey,fixture.approved(),(revision,hash)->journal.renewed(work,revision,hash));require(lease.revision()==2);
            lease.renew(next,fixture.publicKey,fixture.approved(),(revision,hash)->{throw new IOException("REPLAY_MUST_NOT_COMMIT_OR_RESET_WINDOW");});require(lease.revision()==2);
            rejected(()->lease.renew(fixture.wire(2,44),fixture.publicKey,fixture.approved(),(revision,hash)->journal.renewed(work,revision,hash)));require(lease.revision()==2);
            JSONObject altered=MeshJson.parse(fixture.wire(3,44),16384);altered.put("resource_lock_generation","2");byte[] bad=fixture.sign(altered);
            rejected(()->lease.renew(bad,fixture.publicKey,fixture.approved(),(revision,hash)->journal.renewed(work,revision,hash)));require(lease.revision()==2);
            journal.stopping(work);journal.cleaned(work,fixture.hash,true,true,true);
        }
    }
    private void workspace(File scratch)throws Exception{
        File root=new File(scratch,"engine-work");
        try(MeshWorkspaceOwnership scope=MeshWorkspaceOwnership.bind(planned->{throw new IOException("SYNTHETIC_JOURNAL_WRITE_FAILED");})){
            rejected(()->new EngineWorkspace(root,()->{}));
        }
        require(root.listFiles()!=null&&root.listFiles().length==0);
        try(EngineWorkspace normal=new EngineWorkspace(root,()->{})){require(normal.directory().isDirectory());}
        require(root.listFiles().length==0);
    }
    private void resultCipher(File scratch)throws Exception{
        Fixture fixture=new Fixture();MeshLease lease=fixture.lease(1,20);byte[] plain=new byte[2*65536+37],key=new byte[32];new SecureRandom().nextBytes(plain);new SecureRandom().nextBytes(key);
        File source=new File(scratch,"synthetic.bin"),target=new File(scratch,"output.aead");Files.write(source.toPath(),plain);
        MeshResultCipher.Result result=MeshResultCipher.encrypt(source,target,key,lease,()->{});byte[] cipher=Files.readAllBytes(target.toPath());require(result.ciphertextBytes==cipher.length);require(Arrays.equals(plain,decrypt(cipher,key,lease)));
        cipher[32+17+10]^=1;boolean authenticatedFailure=false;
        try{decrypt(cipher,key,lease);}catch(javax.crypto.AEADBadTagException expected){authenticatedFailure=true;}require(authenticatedFailure);
        Arrays.fill(plain,(byte)0);Arrays.fill(key,(byte)0);Arrays.fill(cipher,(byte)0);
    }
    private byte[] decrypt(byte[] encoded,byte[] key,MeshLease lease)throws Exception{
        ByteBuffer input=ByteBuffer.wrap(encoded);byte[] magic=new byte[8];input.get(magic);require(Arrays.equals(magic,"MESHRES1".getBytes(StandardCharsets.US_ASCII)));int chunk=input.getInt();long size=input.getLong();byte[] prefix=new byte[8];input.get(prefix);int chunks=input.getInt();require(chunk==65536&&size>0&&size<1024*1024&&chunks==(size+chunk-1)/chunk);
        byte[] context=("P11_ANDROID_RESULT_AESGCM_1\n"+lease.id+"\n"+lease.attempt+"\n"+lease.resource+"\n"+lease.generation+"\n"+lease.fence+"\n"+lease.manifestHash+"\n").getBytes(StandardCharsets.US_ASCII);
        ByteArrayOutputStream output=new ByteArrayOutputStream();
        for(int i=0;i<chunks;i++){
            byte[] index=new byte[17];input.get(index);ByteBuffer fields=ByteBuffer.wrap(index);require(fields.getInt()==i);int count=fields.getInt();require(fields.get()==(byte)(i==chunks-1?1:0)&&fields.getLong()==size&&count>0&&count<=chunk);
            byte[] body=new byte[count+16];input.get(body);Cipher aes=Cipher.getInstance("AES/GCM/NoPadding");aes.init(Cipher.DECRYPT_MODE,new SecretKeySpec(key,"AES"),new GCMParameterSpec(128,ByteBuffer.allocate(12).put(prefix).putInt(i).array()));aes.updateAAD(context);aes.updateAAD(index);byte[] clear=aes.doFinal(body);output.write(clear);Arrays.fill(clear,(byte)0);
        }
        require(!input.hasRemaining()&&output.size()==size);return output.toByteArray();
    }
    private void unknownReport(File scratch)throws Exception{
        Fixture fixture=new Fixture();MeshLease lease=fixture.lease(1,20);
        // An owned, ephemeral listener drops replies. Never call another app's fixed port.
        try(ServerSocket listener=new ServerSocket(0,2,InetAddress.getByName("127.0.0.1"))){
        java.util.concurrent.atomic.AtomicInteger progressRequests=new java.util.concurrent.atomic.AtomicInteger();
        listener.setSoTimeout(500);Thread dropper=new Thread(()->{while(!listener.isClosed())try(Socket accepted=listener.accept()){accepted.setSoTimeout(1000);replyOrDrop(accepted,progressRequests);}catch(SocketTimeoutException again){/* Bounded owner stop check. */}catch(IOException closed){if(listener.isClosed())return;}},"Mesh-test-owned-dropper");dropper.setDaemon(true);dropper.start();
        try(MeshDeviceIdentity identity=new MeshDeviceIdentity(scratch,true);MeshOwnerConfig store=new MeshOwnerConfig(scratch,true);MeshControlClient control=new MeshControlClient(fixture.config(identity.publicKey()).put("control_origin","http://127.0.0.1:"+listener.getLocalPort()),identity)){
            MeshControlReporter reports=new MeshControlReporter(store,identity,control);rejected(()->reports.progress(lease,"ACKED",null));JSONObject first=store.controlRecords().getJSONObject("rows").getJSONObject("progress:"+lease.id);
            rejected(()->reports.progress(lease,"RUNNING",0.5));JSONObject second=store.controlRecords().getJSONObject("rows").getJSONObject("progress:"+lease.id);require(first.getString("key").equals(second.getString("key"))&&first.getString("body").equals(second.getString("body")));
            int requestsBeforeCleanup=progressRequests.get();
            JSONObject receipt=new JSONObject().put("attempt_id",lease.attempt).put("resource_id",lease.resource).put("resource_lock_generation",lease.generation).put("fencing_token",lease.fence).put("metadata_sha256",lease.metadataHash).put("phase","CLEANED").put("cleanup_manifest_hash",fixture.hash);
            reports.cleanup(lease,receipt);reports.retryPending();JSONObject terminal=store.controlRecords().getJSONObject("rows").getJSONObject("progress:"+lease.id);
            require("ack_fixture_cleanup".equals(terminal.getString("superseded_by_cleanup"))&&!terminal.has("ack")&&progressRequests.get()==requestsBeforeCleanup);
            require(first.getString("key").equals(terminal.getString("key"))&&first.getString("body").equals(terminal.getString("body")));
        }finally{listener.close();dropper.join(5000);require(!dropper.isAlive());}}
    }
    private static void replyOrDrop(Socket socket,java.util.concurrent.atomic.AtomicInteger progress)throws IOException{
        InputStream input=socket.getInputStream();ByteArrayOutputStream header=new ByteArrayOutputStream();int value;
        while((value=input.read())!=-1){header.write(value);if(header.size()>8192)throw new IOException("SYNTHETIC_HEADER_LIMIT");byte[] bytes=header.toByteArray();int n=bytes.length;
            if(n>=4&&bytes[n-4]=='\r'&&bytes[n-3]=='\n'&&bytes[n-2]=='\r'&&bytes[n-1]=='\n')break;
        }
        String request=new String(header.toByteArray(),StandardCharsets.US_ASCII);java.util.regex.Matcher length=java.util.regex.Pattern.compile("(?im)^Content-Length: ([0-9]+)\\r?$").matcher(request);
        if(!length.find())throw new IOException("SYNTHETIC_BODY_LENGTH");int remaining=Integer.parseInt(length.group(1));if(remaining>4096)throw new IOException("SYNTHETIC_BODY_LIMIT");
        byte[] discard=new byte[1024];while(remaining>0){int count=input.read(discard,0,Math.min(discard.length,remaining));if(count<0)throw new EOFException();remaining-=count;}
        if(!request.startsWith("POST /v1/p11/runner/attempts/att_fixture/cleanup ")){progress.incrementAndGet();socket.setSoLinger(true,0);return;}
        byte[] reply=("{\"id\":\"ack_fixture_cleanup\",\"status\":\"CLEANED\",\"version\":1,\"recorded_at\":\""+Instant.now()+"\"}").getBytes(StandardCharsets.US_ASCII);
        OutputStream output=socket.getOutputStream();output.write(("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "+reply.length+"\r\nConnection: close\r\n\r\n").getBytes(StandardCharsets.US_ASCII));output.write(reply);output.flush();
    }
    private static final class Fixture {
        final byte[] seed=new byte[32],publicKey;final String hash;final Instant issued=Instant.ofEpochSecond(Instant.now().getEpochSecond());final JSONObject approval,manifest;
        Fixture()throws Exception{
            Arrays.fill(seed,(byte)7);publicKey=new Ed25519PrivateKeyParameters(seed,0).generatePublicKey().getEncoded();char[] hex=new char[64];Arrays.fill(hex,'a');hash=new String(hex);
            approval=new JSONObject().put("order_id","ord_fixture").put("route_id","route_fixture").put("seller_id","usr_seller").put("device_id","dev_fixture").put("resource_id","res_fixture").put("capability_id","cap_fixture").put("contract_hash",hash).put("kernel_environment_digest",hash)
                .put("bundle_id","bundle_fixture").put("bundle_sha256",hash).put("recipe_sha256",hash).put("engine_id","sd_cpp").put("engine_build_sha256",hash).put("backend","cpu").put("task_type","image.generate").put("model_alias","image_fixture").put("operation","OPENAI_IMAGES_GENERATIONS_V1")
                .put("maximum_width",64).put("maximum_height",64).put("maximum_frames",1).put("maximum_seconds",1).put("mode","PLAINTEXT").put("privacy","STANDARD");
            manifest=MeshJson.copy(approval);manifest.put("profile_id",MeshLease.MANIFEST_PROFILE).put("job_id","job_fixture").put("attempt_id","att_fixture").put("resource_lock_generation","1").put("fencing_token","1").put("input_asset_ref","asset_fixture").put("input_ciphertext_sha256",hash).put("input_bytes",1).put("request_payload_sha256",hash).put("input_assets",new JSONArray()).put("hard_deadline",issued.plusSeconds(120).toString());
        }
        String pin(){return MeshDeviceIdentity.encoded(publicKey);}
        MeshLease.Approval approved()throws Exception{return new MeshLease.Approval(approval);}
        JSONObject binding()throws Exception{return new JSONObject().put("seller_id","usr_seller").put("device_id","dev_fixture").put("resource_id","res_fixture");}
        MeshLease lease(int revision,int seconds)throws Exception{return MeshLease.verify(wire(revision,seconds),manifest.toString().getBytes(StandardCharsets.UTF_8),publicKey,approved());}
        byte[] wire(int revision,int seconds)throws Exception{
            JSONObject lease=new JSONObject().put("lease_id","lease_fixture").put("job_id","job_fixture").put("attempt_id","att_fixture").put("order_id","ord_fixture").put("seller_id","usr_seller").put("device_id","dev_fixture").put("resource_id","res_fixture").put("resource_lock_generation","1").put("capability_id","cap_fixture")
                .put("execution_manifest_ref","manifest_fixture").put("execution_manifest_hash",MeshJournal.sha(manifest.toString().getBytes(StandardCharsets.UTF_8))).put("kernel_environment_digest",hash).put("lease_revision",revision).put("issued_at",issued.toString()).put("not_before",issued.toString()).put("hard_deadline",issued.plusSeconds(120).toString()).put("fencing_token","1").put("expires_at",issued.plusSeconds(seconds).toString()).put("contract_hash",hash).put("input_asset_refs",new JSONArray().put("asset_fixture")).put("signature_profile_id",MeshLease.LEASE_PROFILE);
            return sign(lease);
        }
        byte[] sign(JSONObject lease)throws Exception{
            List<String> fields=new ArrayList<>();fields.add(MeshLease.LEASE_PROFILE);
            for(String field:new String[]{"lease_id","job_id","attempt_id","order_id","seller_id","device_id","resource_id","resource_lock_generation","capability_id","execution_manifest_ref","execution_manifest_hash","kernel_environment_digest","lease_revision","issued_at","not_before","hard_deadline","fencing_token","expires_at","contract_hash"})fields.add(lease.get(field).toString());
            JSONArray assets=lease.getJSONArray("input_asset_refs");fields.add(Integer.toString(assets.length()));for(int i=0;i<assets.length();i++)fields.add(assets.getString(i));fields.add(lease.optString("envelope_ref",""));byte[] message=(String.join("\n",fields)+"\n").getBytes(StandardCharsets.US_ASCII);
            Ed25519Signer signer=new Ed25519Signer();signer.init(true,new Ed25519PrivateKeyParameters(seed,0));signer.update(message,0,message.length);lease.put("signature",MeshDeviceIdentity.encoded(signer.generateSignature()));return lease.toString().getBytes(StandardCharsets.UTF_8);
        }
        JSONObject config(String localPublic)throws Exception{
            JSONObject conditions=new JSONObject().put("charging_only",true).put("wifi_only",true).put("allow_screen_on",true).put("minimum_battery_percent",30).put("maximum_thermal_status",3).put("reserved_ram_bytes",536870912L).put("estimated_peak_ram_bytes",1073741824L).put("minimum_free_disk_bytes",536870912L);
            return new JSONObject().put("profile",MeshOwnerConfig.PROFILE).put("control_origin","http://127.0.0.1:9").put("allow_development_http",true).put("issuer_public_key",pin()).put("device_public_key",localPublic).put("device_version",1).put("approval",approval).put("conditions",conditions);
        }
    }
    private void removeTestTree(File root)throws Exception{
        Path allowed=getTargetContext().getNoBackupFilesDir().getCanonicalFile().toPath(),target=root.getCanonicalFile().toPath();require(target.getParent().equals(allowed)&&root.getName().startsWith("mesh-boundaries-"));
        try(java.util.stream.Stream<Path> files=Files.walk(target)){
            for(Path anchor:(Iterable<Path>)files.filter(path->path.getFileName().toString().equals("anchor.json"))::iterator){
                JSONObject metadata=MeshJson.parse(Files.readAllBytes(anchor),4096);String installation=metadata.optString("installation");
                if(installation.matches("[a-f0-9]{32}")){KeyStore keys=KeyStore.getInstance("AndroidKeyStore");keys.load(null);keys.deleteEntry("p11_mesh_"+installation);}
            }
        }
        try(java.util.stream.Stream<Path> paths=Files.walk(target)){for(Path path:(Iterable<Path>)paths.sorted(Comparator.reverseOrder())::iterator)Files.delete(path);}
    }
}
