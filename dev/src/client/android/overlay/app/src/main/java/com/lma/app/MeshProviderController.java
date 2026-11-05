package com.lma.app;

import android.os.Build;
import com.lma.app.mesh.*;
import org.json.JSONObject;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

/** One service-owned control worker. Local UI exposes metadata and choices, never task bodies. */
public final class MeshProviderController implements AutoCloseable {
    private final LmaApp app;
    private final EngineHost resident;
    private final Runnable changed;
    private final ExecutorService worker=Executors.newSingleThreadExecutor(r->{Thread thread=new Thread(r,"Mesh-original-owner");thread.setDaemon(true);return thread;});
    private final AtomicBoolean busy=new AtomicBoolean();
    private final AtomicLong ownerChoice=new AtomicLong();
    private volatile boolean enabled,closed;
    private volatile String state="DISABLED",reason="";
    private MeshDeviceIdentity identity;
    private MeshOwnerConfig config;
    private MeshJournal journal;
    private volatile MeshSharePolicy policy;
    private volatile MeshMediaRunner runner;
    private MeshControlClient control;
    private MeshControlReporter reports;
    private MeshMediaRecovery recovery;
    private JSONObject approved;
    public MeshProviderController(LmaApp app,EngineHost resident,Runnable changed){this.app=app;this.resident=resident;this.changed=changed;}
    private interface Action<T>{T run()throws Exception;}
    private <T> CompletableFuture<T> enqueue(Action<T> action){
        CompletableFuture<T> result=new CompletableFuture<>();
        if(closed){result.completeExceptionally(new IOException("MESH_PROVIDER_CLOSED"));return result;}
        try{worker.execute(()->{try{if(closed)throw new IOException("MESH_PROVIDER_CLOSED");result.complete(action.run());}catch(Exception error){reason=safeCode(error);result.completeExceptionally(new IOException(reason));}finally{changed.run();}});}catch(RejectedExecutionException stopped){result.completeExceptionally(new IOException("MESH_PROVIDER_CLOSED"));}
        return result;
    }
    private void openIdentity(boolean initialize)throws Exception{
        if(identity==null)identity=new MeshDeviceIdentity(app.getNoBackupFilesDir(),initialize);
        if(config==null)config=new MeshOwnerConfig(app.getNoBackupFilesDir(),initialize);
    }
    /** Explicit owner action only. Opening the app/service never creates an identity or enables sharing. */
    public CompletableFuture<String> initializeIdentity(){return enqueue(()->{openIdentity(true);return identity.publicKey();});}
    public CompletableFuture<String> publicKey(){return enqueue(()->{openIdentity(false);return identity.publicKey();});}
    public CompletableFuture<JSONObject> describe(String bundle,String task){return enqueue(()->new MeshLmaDescriptors(app).describe(bundle,task));}
    public CompletableFuture<JSONObject> importApproval(byte[] bytes){
        byte[] own=bytes.clone();return enqueue(()->{try{
            if(enabled||busy.get()||journal!=null&&journal.held())throw new IOException("MESH_DISABLE_AND_CLEAN_BEFORE_APPROVAL");openIdentity(false);
            JSONObject proposed=MeshOwnerConfig.validate(MeshJson.parse(own,65536),identity.publicKey());validateLocal(proposed);new MeshSharePolicy(app,proposed.getJSONObject("conditions"));
            if(journal!=null){journal.close();journal=null;}if(control!=null){control.close();control=null;}reports=null;runner=null;
            config.importApproved(own,identity.publicKey());approved=proposed;recovery=null;state="APPROVED";reason="";return status();
        }finally{Arrays.fill(own,(byte)0);}}).whenComplete((value,error)->Arrays.fill(own,(byte)0));
    }
    private void validateLocal(JSONObject values)throws Exception{
        JSONObject approval=values.getJSONObject("approval"),actual=new MeshLmaDescriptors(app).describe(approval.getString("bundle_id"),approval.getString("task_type"));
        for(String key:new String[]{"bundle_sha256","recipe_sha256","engine_id","engine_build_sha256","backend","task_type"})if(!approval.getString(key).equals(actual.getString(key)))throw new IOException("MESH_LOCAL_KERNEL_APPROVAL_MISMATCH");
    }
    private void openApproved()throws Exception{
        openIdentity(false);approved=config.load(identity.publicKey());validateLocal(approved);JSONObject approval=approved.getJSONObject("approval");
        if(journal==null)journal=new MeshJournal(app.getNoBackupFilesDir(),new JSONObject().put("seller_id",approval.getString("seller_id")).put("device_id",approval.getString("device_id")).put("resource_id",approval.getString("resource_id")),true);
        if(control==null)control=new MeshControlClient(approved,identity);if(reports==null)reports=new MeshControlReporter(config,identity,control);
    }
    public CompletableFuture<JSONObject> enable(){long choice=ownerChoice.incrementAndGet();return enqueue(()->{
        if(choice!=ownerChoice.get())throw new IOException("MESH_OWNER_CHOICE_CHANGED");
        if(busy.get())throw new IOException("MESH_PHONE_SLOT_BUSY");openApproved();if(journal.held()){state="QUARANTINED";throw new IOException("MESH_ORIGINAL_OWNER_REQUIRES_RECOVERY");}
        if(runner!=null)runner.close();policy=new MeshSharePolicy(app,approved.getJSONObject("conditions"));runner=new MeshMediaRunner(app,resident,journal,policy);
        if(choice!=ownerChoice.get()){runner.close();throw new IOException("MESH_OWNER_CHOICE_CHANGED");}
        enabled=true;policy.ownerEnabled(true);state="READY";reason="";return status();
    });}
    public CompletableFuture<JSONObject> disable(){
        ownerChoice.incrementAndGet();enabled=false;MeshSharePolicy current=policy;if(current!=null)current.ownerEnabled(false);
        return enqueue(()->{if(runner!=null)runner.close();state=journal!=null&&journal.held()?"QUARANTINED":"DISABLED";return status();});
    }
    /** Called by the private control/data adapter, never by an exported Intent or a WebView bridge. */
    public CompletableFuture<JSONObject> execute(byte[] rawLease,byte[] rawManifest,byte[] request,byte[] recipientKey,MeshMediaAssets.Source inputs){
        CompletableFuture<JSONObject> rejected=new CompletableFuture<>();
        if(closed||!enabled||recipientKey==null||recipientKey.length!=32||rawLease==null||rawManifest==null||request==null||rawLease.length>16384||rawManifest.length>16384||request.length>256*1024||!busy.compareAndSet(false,true)){
            if(recipientKey!=null)Arrays.fill(recipientKey,(byte)0);rejected.completeExceptionally(new IOException("MESH_ADMISSION_NOT_READY"));return rejected;
        }
        byte[] leaseBytes=rawLease.clone(),manifestBytes=rawManifest.clone(),body=request.clone(),key=recipientKey.clone();Arrays.fill(recipientKey,(byte)0);
        CompletableFuture<JSONObject> future=enqueue(()->{
            MeshLease lease=null;
            try{
                if(!enabled||runner==null)throw new IOException("MESH_OWNER_SHARING_DISABLED");
                byte[] pin=MeshDeviceIdentity.decoded(approved.getString("issuer_public_key"),32);
                try{lease=MeshLease.verify(leaseBytes,manifestBytes,pin,new MeshLease.Approval(approved.getJSONObject("approval")));}finally{Arrays.fill(pin,(byte)0);}
                if(lease.revision()!=1)throw new IOException("MESH_FIRST_SUBMISSION_REVISION");
                final MeshLease original=lease;state="EXECUTING";changed.run();
                // ACK follows durable local ownership, before the first model submission.
                final boolean[] admitted={false};
                MeshResultCipher.Result result;
                try(MeshProgressPump progress=new MeshProgressPump(original,reports,control,()->runner.cancel(original.id))){
                    result=runner.execute(original,body,key,inputs,(id,stage,fraction)->{
                        if(!admitted[0]){reports.progress(original,"ACKED",null);admitted[0]=true;}else progress.changed(fraction);
                    });progress.check();
                }
                return new JSONObject().put("lease_id",original.id).put("attempt_id",original.attempt).put("ciphertext_sha256",result.ciphertextSha256).put("ciphertext_bytes",result.ciphertextBytes).put("charge_authority",false);
            }finally{
                Arrays.fill(body,(byte)0);Arrays.fill(key,(byte)0);Arrays.fill(leaseBytes,(byte)0);Arrays.fill(manifestBytes,(byte)0);
                if(lease!=null){try{JSONObject receipt=journal.receipt(lease.id);reports.cleanup(lease,receipt);}catch(Exception pending){reason=safeCode(pending);}}
                state=journal!=null&&journal.held()?"QUARANTINED":enabled?"READY":"DISABLED";
            }
        });
        future.whenComplete((value,error)->{Arrays.fill(body,(byte)0);Arrays.fill(key,(byte)0);Arrays.fill(leaseBytes,(byte)0);Arrays.fill(manifestBytes,(byte)0);busy.set(false);changed.run();});return future;
    }
    /** Private control transport continuation. Never callable as a UI Intent action or a second model submission. */
    public void renewExecutionLease(String originalLease,byte[] signedLease)throws Exception{
        MeshMediaRunner current=runner;JSONObject approval=approved;
        if(closed||!enabled||!busy.get()||current==null||approval==null)throw new IOException("MESH_RENEWAL_ORIGINAL_OWNER");
        byte[] pin=MeshDeviceIdentity.decoded(approval.getString("issuer_public_key"),32),wire=signedLease==null?null:signedLease.clone();
        try{if(wire==null||wire.length>16384)throw new IOException("MESH_RENEWAL_BODY");current.renew(originalLease,wire,pin,new MeshLease.Approval(approval.getJSONObject("approval")));}finally{Arrays.fill(pin,(byte)0);if(wire!=null)Arrays.fill(wire,(byte)0);}
    }
    public CompletableFuture<JSONObject> recoverAfterReboot(){return enqueue(()->{
        if(enabled||busy.get())throw new IOException("MESH_DISABLE_BEFORE_RECOVERY");openApproved();if(recovery==null)recovery=new MeshMediaRecovery(app,resident,journal);recovery.afterReboot();runner=null;state="DISABLED";reason="";return status();
    });}
    public CompletableFuture<JSONObject> retryOwnedCleanup(){return enqueue(()->{
        if(enabled||busy.get()||runner==null)throw new IOException("MESH_DISABLE_BEFORE_RECOVERY");runner.retryCleanup();state="DISABLED";reason="";return status();
    });}
    public CompletableFuture<JSONObject> retryReports(){return enqueue(()->{openApproved();return reports.retryPending();});}
    public boolean keepAlive(){return enabled||busy.get();}
    public JSONObject status(){
        try{return new JSONObject().put("profile","P11_ANDROID_PROVIDER_STATUS_1").put("model_name",Build.MANUFACTURER+" "+Build.MODEL).put("state",state).put("sharing_enabled",enabled).put("busy",busy.get()).put("safe_reason_code",reason).put("content_preview_available",false).put("market_qualified",false);}
        catch(Exception impossible){throw new IllegalStateException("MESH_STATUS");}
    }
    private static String safeCode(Exception error){String value=error.getMessage();return value!=null&&value.matches("MESH_[A-Z0-9_]{1,96}")?value:"MESH_CONTROL_ACTION_FAILED";}
    @Override public void close(){
        if(closed)return;closed=true;ownerChoice.incrementAndGet();enabled=false;MeshSharePolicy current=policy;if(current!=null)current.ownerEnabled(false);MeshControlClient connection=control;if(connection!=null)connection.close();
        try{worker.execute(()->{try{
            if(runner!=null)runner.close();if(journal!=null&&journal.held()){state="QUARANTINED";return;}
            if(journal!=null)journal.close();if(config!=null)config.close();if(identity!=null)identity.close();state="DISABLED";
        }catch(Exception error){state="QUARANTINED";reason=safeCode(error);}finally{changed.run();}});}catch(RejectedExecutionException stopped){state="QUARANTINED";}
        worker.shutdown();
    }
}
