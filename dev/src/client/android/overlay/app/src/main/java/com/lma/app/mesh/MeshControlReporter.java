package com.lma.app.mesh;

import org.json.JSONObject;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.util.Arrays;

/** Persist original report bytes before I/O; retries use fresh proofs and the same body/key. */
public final class MeshControlReporter {
    public interface Continuation { void apply(MeshLease original,byte[] signedLease)throws Exception; }
    private final MeshOwnerConfig store;
    private final MeshDeviceIdentity identity;
    private final MeshControlClient client;
    private final JSONObject records;
    private final Continuation continuation;
    private boolean poisoned;
    public MeshControlReporter(MeshOwnerConfig store,MeshDeviceIdentity identity,MeshControlClient client)throws Exception{
        this(store,identity,client,null);
    }
    public MeshControlReporter(MeshOwnerConfig store,MeshDeviceIdentity identity,MeshControlClient client,Continuation continuation)throws Exception{
        this.continuation=continuation;
        this.store=store;this.identity=identity;this.client=client;records=store.controlRecords();
        MeshJson.fields(records,"profile","rows","sequences");
        if(!"P11_ANDROID_CONTROL_OUTBOX_1".equals(records.getString("profile"))||records.getJSONObject("rows").length()>32||records.getJSONObject("sequences").length()>64)throw new IOException("MESH_CONTROL_RECORDS");
        JSONObject rows=records.getJSONObject("rows");
        for(java.util.Iterator<String> it=rows.keys();it.hasNext();){String name=it.next();JSONObject row=rows.getJSONObject(name);MeshJson.fields(row,"path","key","body","ack","superseded_by_cleanup");
            if(!name.matches("(?:progress|cleanup|recovery):[A-Za-z][A-Za-z0-9_-]{2,127}")||!row.getString("key").matches("m[pcr]_[a-f0-9]{64}(?:_[1-9][0-9]{0,18})?")||!row.getString("path").matches("/v1/p11/runner/attempts/[A-Za-z][A-Za-z0-9_-]{2,127}/(?:progress|cleanup)"))throw new IOException("MESH_CONTROL_RECORD_OWNER");
            byte[] body=decodeBody(row);try{JSONObject value=MeshJson.parse(body,4096);boolean cleanup=!name.startsWith("progress:");
                if(!name.substring(name.indexOf(':')+1).equals(MeshLease.identifier(value,"lease_id"))||cleanup!=row.getString("path").endsWith("/cleanup"))throw new IOException("MESH_CONTROL_RECORD_OWNER");
                MeshLease.counter(value,"fencing_token");
                if(cleanup){MeshJson.fields(value,"lease_id","fencing_token","cleanup_manifest_hash","state","work_stopped","tokens_revoked","workspace_cleared","recorded_at","signature_profile_id","signature","recovery_of");
                    boolean recovery=name.startsWith("recovery:");if(recovery!=value.has("recovery_of")||!value.getString("signature_profile_id").equals(recovery?"P11_DEVICE_CLEANUP_RECOVERY_1":"P11_DEVICE_CLEANUP_1"))throw new IOException("MESH_RECOVERY_PROFILE");
                    if(recovery){MeshLease.identifier(value,"recovery_of");if(!"ACKED".equals(value.getString("state"))||!value.getBoolean("work_stopped")||!value.getBoolean("tokens_revoked")||!value.getBoolean("workspace_cleared"))throw new IOException("MESH_RECOVERY_FACTS");}}
                else{MeshJson.fields(value,"lease_id","fencing_token","sequence","phase","fraction");MeshLease.counter(value,"sequence");}
            }finally{Arrays.fill(body,(byte)0);}
            if(row.has("ack")&&!(row.get("ack")instanceof JSONObject))throw new IOException("MESH_CONTROL_RECORD_ACK");
            if(row.has("superseded_by_cleanup")&&(!name.startsWith("progress:")||!MeshJson.string(row,"superseded_by_cleanup").matches("[A-Za-z][A-Za-z0-9_-]{2,127}")))throw new IOException("MESH_CONTROL_REPORT_TERMINAL");
        }
    }
    private static byte[] decodeBody(JSONObject row)throws Exception{
        String text=MeshJson.string(row,"body");byte[] body=android.util.Base64.decode(text,android.util.Base64.URL_SAFE|android.util.Base64.NO_WRAP|android.util.Base64.NO_PADDING);
        if(body.length>4096||!MeshDeviceIdentity.encoded(body).equals(text))throw new IOException("MESH_CONTROL_RECORD_BODY");return body;
    }
    private void save()throws Exception{
        if(poisoned)throw new IOException("MESH_CONTROL_RECORDS_POISONED");
        try{store.controlRecords(records);}catch(Exception failure){poisoned=true;throw new IOException("MESH_CONTROL_RECORD_WRITE",failure);}
    }
    private JSONObject freeze(String name,String path,String key,byte[] body)throws Exception{
        if(poisoned||body.length>4096)throw new IOException("MESH_CONTROL_RECORDS_POISONED");JSONObject rows=records.getJSONObject("rows");
        if(!rows.has(name)&&rows.length()>=32)throw new IOException("MESH_CONTROL_HISTORY_LIMIT");
        JSONObject row=new JSONObject().put("path",path).put("key",key).put("body",MeshDeviceIdentity.encoded(body));rows.put(name,row);save();return row;
    }
    private JSONObject send(JSONObject row,boolean progress)throws Exception{
        if(poisoned)throw new IOException("MESH_CONTROL_RECORDS_POISONED");if(row.has("ack"))return MeshJson.copy(row.getJSONObject("ack"));
        byte[] body=decodeBody(row);MeshControlClient.Reply reply;
        try{reply=client.request("POST",row.getString("path"),row.getString("key"),body,32768);}finally{Arrays.fill(body,(byte)0);}
        if(reply.status!=200)throw new IOException("MESH_CONTROL_REPORT_UNACKNOWLEDGED");
        JSONObject result;
        try{result=MeshJson.parse(reply.body,32768);}finally{Arrays.fill(reply.body,(byte)0);}
        JSONObject ack=progress?result.getJSONObject("ack"):result;
        if(progress){MeshJson.fields(result,"ack","action","lease");String action=MeshJson.string(result,"action");if(!Arrays.asList("NO_CHANGE","STOP","CONTINUE").contains(action)||"CONTINUE".equals(action)!=result.has("lease")||result.has("lease")&&!(result.get("lease") instanceof JSONObject))throw new IOException("MESH_REPORT_CONTINUATION_SHAPE");}
        MeshJson.fields(ack,"id","status","version","recorded_at");MeshLease.identifier(ack,"id");MeshJson.integer(ack,"version",1,1);Instant.parse(MeshJson.string(ack,"recorded_at"));
        if(progress?!Arrays.asList("ACKED","RECORDED","STOP").contains(ack.getString("status")):!Arrays.asList("CLEANED","QUARANTINED").contains(ack.getString("status")))throw new IOException("MESH_CONTROL_ACK_STATUS");
        row.put("ack",result);save();return MeshJson.copy(result);
    }
    private void applyAction(MeshLease original,JSONObject result)throws Exception{
        String action=result.getString("action");if("STOP".equals(action))throw new IOException("MESH_PLATFORM_STOP_REQUIRED");
        if("CONTINUE".equals(action)){
            if(continuation==null)throw new IOException("MESH_INDEPENDENT_CONTINUATION_REQUIRED");
            byte[] wire=result.getJSONObject("lease").toString().getBytes(StandardCharsets.UTF_8);
            try{if(wire.length>16384)throw new IOException("MESH_RENEWAL_BODY");continuation.apply(original,wire);}finally{Arrays.fill(wire,(byte)0);}
        }
    }
    public synchronized void progress(MeshLease lease,String phase,Double fraction)throws Exception{
        lease.check();if(!Arrays.asList("ACKED","RUNNING","UPLOADING","FAILED","CANCELED").contains(phase)||fraction!=null&&(!Double.isFinite(fraction)||fraction<0||fraction>1))throw new IOException("MESH_PROGRESS_SHAPE");
        String name="progress:"+lease.id;JSONObject rows=records.getJSONObject("rows"),old=rows.optJSONObject(name);
        if(old!=null&&old.has("superseded_by_cleanup"))throw new IOException("MESH_REPORT_ORIGINAL_TERMINAL");
        // Recover an ACK persisted before local renewal commit, as well as a
        // lost HTTP reply. Independent verification keeps replay idempotent.
        if(old!=null)applyAction(lease,send(old,true));
        JSONObject counters=records.getJSONObject("sequences");long previous=counters.has(lease.id)?MeshJson.integer(counters,lease.id,1,Long.MAX_VALUE):0;
        if(previous==Long.MAX_VALUE||!counters.has(lease.id)&&counters.length()>=64)throw new IOException("MESH_REPORT_SEQUENCE_EXHAUSTED");long sequence=previous+1;
        JSONObject body=new JSONObject().put("lease_id",lease.id).put("fencing_token",Long.toString(lease.fence)).put("sequence",Long.toString(sequence)).put("phase",phase);
        if(fraction!=null)body.put("fraction",fraction);counters.put(lease.id,sequence);
        String hash=MeshJournal.sha((lease.id+"\n"+lease.attempt+"\n"+lease.generation+"\n").getBytes(StandardCharsets.US_ASCII));
        JSONObject row=freeze(name,"/v1/p11/runner/attempts/"+lease.attempt+"/progress","mp_"+hash+"_"+sequence,body.toString().getBytes(StandardCharsets.UTF_8));
        applyAction(lease,send(row,true));
    }
    public synchronized JSONObject cleanup(MeshLease lease,JSONObject localReceipt)throws Exception{
        if(!lease.attempt.equals(localReceipt.getString("attempt_id"))||!lease.resource.equals(localReceipt.getString("resource_id"))||lease.generation!=localReceipt.getLong("resource_lock_generation")||lease.fence!=localReceipt.getLong("fencing_token")||!lease.metadataHash.equals(localReceipt.getString("metadata_sha256")))throw new IOException("MESH_CLEANUP_ORIGINAL_OWNER");
        String name="cleanup:"+lease.id;JSONObject row=records.getJSONObject("rows").optJSONObject(name);
        if(row==null){
            boolean cleaned="CLEANED".equals(localReceipt.getString("phase"));
            if(!cleaned&&!"QUARANTINED".equals(localReceipt.getString("phase")))throw new IOException("MESH_CLEANUP_NOT_STOPPED");
            String hash=cleaned?MeshLease.digest(localReceipt,"cleanup_manifest_hash"):MeshJournal.sha(("P11_ANDROID_QUARANTINED_1\n"+lease.id+"\n"+lease.generation+"\n"+lease.fence+"\n"+lease.metadataHash+"\n").getBytes(StandardCharsets.US_ASCII));
            String unsigned="{\"lease_id\":\""+lease.id+"\",\"fencing_token\":\""+lease.fence+"\",\"cleanup_manifest_hash\":\""+hash+"\",\"state\":\""+(cleaned?"ACKED":"FAILED")+"\",\"work_stopped\":"+cleaned+",\"tokens_revoked\":"+cleaned+",\"workspace_cleared\":"+cleaned+",\"recorded_at\":\""+utcNano(Instant.now())+"\",\"signature_profile_id\":\"P11_DEVICE_CLEANUP_1\",\"signature\":\"\"}";
            String message="P11_DEVICE_CLEANUP_1\n"+lease.device+"\n"+lease.attempt+"\n"+lease.generation+"\n"+unsigned;
            String signature=identity.sign(message.getBytes(StandardCharsets.US_ASCII));String signed=unsigned.substring(0,unsigned.length()-2)+signature+"\"}";
            row=freeze(name,"/v1/p11/runner/attempts/"+lease.attempt+"/cleanup","mc_"+MeshJournal.sha(message.getBytes(StandardCharsets.US_ASCII)),signed.getBytes(StandardCharsets.US_ASCII));
        }
        JSONObject terminal=send(row,false);JSONObject progress=records.getJSONObject("rows").optJSONObject("progress:"+lease.id);
        if(progress!=null&&!progress.has("ack")&&!progress.has("superseded_by_cleanup")){progress.put("superseded_by_cleanup",terminal.getString("id"));save();}
        return terminal;
    }
    public synchronized JSONObject retryPending()throws Exception{
        int sent=0;JSONObject rows=records.getJSONObject("rows");
        // Terminal reports are recovered first. A confirmed original cleanup
        // retires older pending progress without fabricating a progress ACK.
        for(java.util.Iterator<String> it=rows.keys();it.hasNext();){String name=it.next();JSONObject row=rows.getJSONObject(name);if(name.startsWith("cleanup:")){
            boolean pending=!row.has("ack");JSONObject terminal=send(row,false);if(pending)sent++;
            JSONObject progress=rows.optJSONObject("progress:"+name.substring("cleanup:".length()));
            if(progress!=null&&!progress.has("ack")&&!progress.has("superseded_by_cleanup")){progress.put("superseded_by_cleanup",terminal.getString("id"));save();}
        }}
        for(java.util.Iterator<String> it=rows.keys();it.hasNext();){String name=it.next();JSONObject row=rows.getJSONObject(name);if(name.startsWith("recovery:")&&!row.has("ack")){JSONObject ack=send(row,false);if(!"CLEANED".equals(ack.getString("status")))throw new IOException("MESH_RECOVERY_UNACKNOWLEDGED");sent++;}}
        for(java.util.Iterator<String> it=rows.keys();it.hasNext();){String name=it.next();JSONObject row=rows.getJSONObject(name);if(name.startsWith("progress:")&&!row.has("ack")&&!row.has("superseded_by_cleanup")){send(row,true);sent++;}}
        return new JSONObject().put("acknowledged",sent);
    }
    /** Called only after actual local cleanup. The original FAILED report is
     * retained; this freezes a separate signature and key before network I/O. */
    public synchronized JSONObject recoverCleaned(MeshJournal journal,String device)throws Exception{
        if(!device.matches("[A-Za-z][A-Za-z0-9_-]{2,127}"))throw new IOException("MESH_RECOVERY_DEVICE");
        JSONObject rows=records.getJSONObject("rows");java.util.List<String> originals=new java.util.ArrayList<>();for(java.util.Iterator<String> it=rows.keys();it.hasNext();){String name=it.next();if(name.startsWith("cleanup:"))originals.add(name);}
        int confirmed=0;
        for(String name:originals){JSONObject original=rows.getJSONObject(name);byte[] originalBytes=decodeBody(original);JSONObject failure;
            try{failure=MeshJson.parse(originalBytes,4096);}finally{Arrays.fill(originalBytes,(byte)0);}
            if(!"FAILED".equals(failure.getString("state")))continue;
            String lease=MeshLease.identifier(failure,"lease_id");JSONObject facts=journal.receipt(lease);if(!"CLEANED".equals(facts.getString("phase")))continue;
            String attempt=MeshLease.identifier(facts,"attempt_id"),resource=MeshLease.identifier(facts,"resource_id");long generation=MeshJson.integer(facts,"resource_lock_generation",1,Long.MAX_VALUE),fence=MeshJson.integer(facts,"fencing_token",1,Long.MAX_VALUE);
            String metadata=MeshLease.digest(facts,"metadata_sha256"),hash=MeshLease.digest(facts,"cleanup_manifest_hash");
            String failedHash=MeshJournal.sha(("P11_ANDROID_QUARANTINED_1\n"+lease+"\n"+generation+"\n"+fence+"\n"+metadata+"\n").getBytes(StandardCharsets.US_ASCII));
            if(!original.getString("path").equals("/v1/p11/runner/attempts/"+attempt+"/cleanup")||!failure.getString("fencing_token").equals(Long.toString(fence))||!failure.getString("cleanup_manifest_hash").equals(failedHash))throw new IOException("MESH_RECOVERY_ORIGINAL_OWNER");
            JSONObject terminal=send(original,false);if(!"QUARANTINED".equals(terminal.getString("status")))throw new IOException("MESH_RECOVERY_ORIGINAL_FAILURE");String failedReport=MeshLease.identifier(terminal,"id");
            String recoveryName="recovery:"+lease;JSONObject recovery=rows.optJSONObject(recoveryName);
            if(recovery==null){
                String unsigned="{\"lease_id\":\""+lease+"\",\"fencing_token\":\""+fence+"\",\"cleanup_manifest_hash\":\""+hash+"\",\"state\":\"ACKED\",\"work_stopped\":true,\"tokens_revoked\":true,\"workspace_cleared\":true,\"recorded_at\":\""+utcNano(Instant.now())+"\",\"signature_profile_id\":\"P11_DEVICE_CLEANUP_RECOVERY_1\",\"signature\":\"\",\"recovery_of\":\""+failedReport+"\"}";
                String message="P11_DEVICE_CLEANUP_RECOVERY_1\n"+device+"\n"+attempt+"\n"+generation+"\n"+failedReport+"\n"+unsigned;
                String signed=unsigned.replace("\"signature\":\"\"","\"signature\":\""+identity.sign(message.getBytes(StandardCharsets.US_ASCII))+"\"");
                recovery=freeze(recoveryName,original.getString("path"),"mr_"+MeshJournal.sha(message.getBytes(StandardCharsets.US_ASCII)),signed.getBytes(StandardCharsets.US_ASCII));
            }
            JSONObject ack=send(recovery,false);if(!"CLEANED".equals(ack.getString("status")))throw new IOException("MESH_RECOVERY_UNACKNOWLEDGED");confirmed++;
        }
        return new JSONObject().put("recovered_originals",confirmed).put("execution_authority_restored",false);
    }
    private static String utcNano(Instant instant){
        String seconds=Instant.ofEpochSecond(instant.getEpochSecond()).toString();if(instant.getNano()==0)return seconds;
        String fraction=String.format(java.util.Locale.ROOT,"%09d",instant.getNano()).replaceFirst("0+$","");return seconds.substring(0,seconds.length()-1)+"."+fraction+"Z";
    }
}
