package com.lma.app.mesh;

import android.util.AtomicFile;
import org.json.JSONObject;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.UUID;

/** Encrypted metadata-only original-owner journal. Never stores request/media/key plaintext. */
public final class MeshJournal implements AutoCloseable {
    public static final String PROFILE="P11_ANDROID_OWNER_JOURNAL_1";
    private final File directory;
    private final AtomicFile file;
    private final MeshKeystore key;
    private final byte[] aad;
    private final MeshJournalGuard guard;
    private JSONObject state;
    private boolean poisoned;
    private boolean closed;
    public static final class Work {
        public final String lease,attempt,resource,metadataHash;
        public final long generation,fence,localGeneration;
        private Work(MeshLease lease,long local) {
            this.lease=lease.id;attempt=lease.attempt;resource=lease.resource;metadataHash=lease.metadataHash;
            generation=lease.generation;fence=lease.fence;localGeneration=local;
        }
    }
    public MeshJournal(File noBackup,JSONObject binding,boolean initialize) throws Exception {
        MeshJson.fields(binding,"seller_id","device_id","resource_id");
        String bound=PROFILE+"\n"+id(binding,"seller_id")+"\n"+id(binding,"device_id")+"\n"+id(binding,"resource_id")+"\n";
        directory=new File(noBackup,"share-mesh");
        File expected=new File(noBackup.getCanonicalFile(),"share-mesh");
        if (Files.isSymbolicLink(directory.toPath())||!directory.getCanonicalFile().equals(expected)) throw new IOException("MESH_JOURNAL_PATH");
        Files.createDirectories(directory.toPath());
        guard=MeshJournalGuard.acquire(directory);
        try {
        AtomicFile anchor=new AtomicFile(new File(directory,"anchor.json"));file=new AtomicFile(new File(directory,"owner.aead"));
        JSONObject original;
        if (!anchor.getBaseFile().exists()) {
            if (!initialize||file.getBaseFile().exists()||new File(directory,"owner.aead.bak").exists()) throw new IOException("MESH_JOURNAL_NOT_INITIALIZED");
            String installation=UUID.randomUUID().toString().replace("-","");
            original=new JSONObject().put("profile",PROFILE).put("installation",installation).put("binding_sha256",sha(bound.getBytes(StandardCharsets.UTF_8)));
            write(anchor,original.toString().getBytes(StandardCharsets.UTF_8));
            key=new MeshKeystore("p11_mesh_"+installation);key.create();
            aad=(bound+installation+"\n").getBytes(StandardCharsets.UTF_8);
            state=new JSONObject().put("profile",PROFILE).put("binding_sha256",original.getString("binding_sha256"))
                    .put("remote_high",0).put("local_high",0).put("owner","").put("phase","FREE").put("receipts",new JSONObject());
            persist();
        } else {
            original=MeshJson.parse(read(anchor,4096),4096);
            MeshJson.fields(original,"profile","installation","binding_sha256");
            String installation=MeshJson.string(original,"installation");
            if (!PROFILE.equals(original.getString("profile"))||!installation.matches("[a-f0-9]{32}")||!sha(bound.getBytes(StandardCharsets.UTF_8)).equals(original.getString("binding_sha256"))) throw new IOException("MESH_JOURNAL_BINDING");
            key=new MeshKeystore("p11_mesh_"+installation);if (!key.exists()) throw new IOException("MESH_JOURNAL_KEY_LOST");
            aad=(bound+installation+"\n").getBytes(StandardCharsets.UTF_8);
            byte[] clear=key.open(read(file,131100),aad);
            try { state=MeshJson.parse(clear,131072); } finally { Arrays.fill(clear,(byte)0); }
            validate(original.getString("binding_sha256"));
            if (!state.getString("owner").isEmpty()) {state.put("phase","QUARANTINED");persist();}
        }
        } catch(Exception error) {guard.close();throw error;}
    }
    private static String id(JSONObject value,String field) throws Exception {
        String id=MeshJson.string(value,field);if (!id.matches("[A-Za-z][A-Za-z0-9_-]{2,127}"))throw new IOException("MESH_ID");return id;
    }
    public static String sha(byte[] value) throws Exception {
        byte[] bytes=MessageDigest.getInstance("SHA-256").digest(value);StringBuilder text=new StringBuilder(64);
        for (byte b:bytes)text.append(String.format(java.util.Locale.ROOT,"%02x",b&255));return text.toString();
    }
    private static byte[] read(AtomicFile file,int maximum) throws Exception {
        try(InputStream in=file.openRead();ByteArrayOutputStream out=new ByteArrayOutputStream()){
            byte[] buffer=new byte[4096];int n;while((n=in.read(buffer))!=-1){if(out.size()+n>maximum)throw new IOException("MESH_JOURNAL_LIMIT");out.write(buffer,0,n);}return out.toByteArray();
        }
    }
    private static void write(AtomicFile file,byte[] bytes) throws Exception {
        FileOutputStream output=file.startWrite();try{output.write(bytes);file.finishWrite(output);}catch(Exception error){file.failWrite(output);throw error;}
    }
    private void validate(String binding) throws Exception {
        MeshJson.fields(state,"profile","binding_sha256","remote_high","local_high","owner","phase","receipts");
        if (!PROFILE.equals(state.getString("profile"))||!binding.equals(state.getString("binding_sha256"))||state.getJSONObject("receipts").length()>64)throw new IOException("MESH_JOURNAL_CORRUPT");
        MeshJson.integer(state,"remote_high",0,Long.MAX_VALUE);MeshJson.integer(state,"local_high",0,Long.MAX_VALUE);
        String owner=state.getString("owner"),phase=state.getString("phase");
        if (owner.isEmpty()!=phase.equals("FREE")||!java.util.Arrays.asList("FREE","PREPARING","EXECUTING","STOPPING","CLEANING","QUARANTINED").contains(phase))throw new IOException("MESH_JOURNAL_OWNER");
    }
    private void persist() throws Exception {
        if(poisoned||closed)throw new IOException("MESH_JOURNAL_POISONED");
        byte[] clear=state.toString().getBytes(StandardCharsets.UTF_8);
        try{if(clear.length>131072)throw new IOException("MESH_JOURNAL_LIMIT");write(file,key.seal(clear,aad));}
        catch(Exception error){poisoned=true;throw new IOException("MESH_JOURNAL_WRITE",error);}
        finally{Arrays.fill(clear,(byte)0);}
    }
    public synchronized Work admit(MeshLease lease) throws Exception {
        lease.check();if(poisoned||closed)throw new IOException("MESH_JOURNAL_POISONED");
        JSONObject receipts=state.getJSONObject("receipts");
        if(receipts.has(lease.id))throw new IOException("MESH_ORIGINAL_ALREADY_RECEIVED_QUERY_ONLY");
        if(!state.getString("owner").isEmpty()||!state.getString("phase").equals("FREE"))throw new IOException("MESH_ORIGINAL_OWNER_HELD");
        long high=state.getLong("remote_high"),local=state.getLong("local_high");
        if(lease.generation<=high||local==Long.MAX_VALUE||receipts.length()>=64)throw new IOException("MESH_GENERATION_OR_HISTORY_LIMIT");
        Work work=new Work(lease,local+1);
        JSONObject receipt=new JSONObject().put("attempt_id",work.attempt).put("resource_id",work.resource).put("fencing_token",work.fence)
                .put("resource_lock_generation",work.generation).put("local_generation",work.localGeneration).put("metadata_sha256",work.metadataHash).put("phase","PREPARING");
        receipts.put(work.lease,receipt);state.put("remote_high",work.generation).put("local_high",work.localGeneration).put("owner",work.lease).put("phase","PREPARING");persist();return work;
    }
    private JSONObject owned(Work work) throws Exception {
        if(poisoned||closed||!state.getString("owner").equals(work.lease)||state.getLong("local_high")!=work.localGeneration||state.getLong("remote_high")!=work.generation)throw new IOException("MESH_OWNER_FENCE");
        JSONObject receipt=state.getJSONObject("receipts").getJSONObject(work.lease);
        if(!receipt.getString("metadata_sha256").equals(work.metadataHash)||!receipt.getString("attempt_id").equals(work.attempt)||receipt.getLong("fencing_token")!=work.fence)throw new IOException("MESH_ORIGINAL_METADATA");return receipt;
    }
    public synchronized void executing(Work work) throws Exception {
        JSONObject r=owned(work);if(!state.getString("phase").equals("PREPARING"))throw new IOException("MESH_SUBMIT_ONCE");
        r.put("phase","EXECUTING");state.put("phase","EXECUTING");persist();
    }
    public synchronized void stopping(Work work) throws Exception {JSONObject r=owned(work);r.put("phase","STOPPING");state.put("phase","STOPPING");persist();}
    public synchronized void result(Work work,MeshResultCipher.Result result)throws Exception{
        JSONObject r=owned(work);if(!state.getString("phase").equals("EXECUTING")||!result.ciphertext.getName().matches("[a-f0-9]{32}\\.aead"))throw new IOException("MESH_RESULT_OWNER");
        r.put("ciphertext",new JSONObject().put("file",result.ciphertext.getName()).put("sha256",result.ciphertextSha256).put("bytes",result.ciphertextBytes));persist();
    }
    public synchronized void cleaned(Work work,String hash,boolean stopped,boolean revoked,boolean cleared) throws Exception {
        JSONObject r=owned(work);if(!hash.matches("[a-f0-9]{64}")||!stopped||!revoked||!cleared||!state.getString("phase").equals("STOPPING"))throw new IOException("MESH_CLEANUP_NOT_PROVEN");
        r.put("phase","CLEANED").put("cleanup_manifest_hash",hash);state.put("phase","FREE").put("owner","");persist();
    }
    public synchronized void quarantine(Work work) throws Exception {JSONObject r=owned(work);r.put("phase","QUARANTINED");state.put("phase","QUARANTINED");persist();}
    public synchronized boolean held() throws Exception {if(poisoned||closed)throw new IOException("MESH_JOURNAL_POISONED");return !state.getString("owner").isEmpty();}
    public synchronized JSONObject receipt(String lease) throws Exception {
        if(closed||poisoned)throw new IOException("MESH_JOURNAL_POISONED");
        if(!lease.matches("[A-Za-z][A-Za-z0-9_-]{2,127}"))throw new IOException("MESH_ID");
        JSONObject r=state.getJSONObject("receipts").optJSONObject(lease);if(r==null)throw new IOException("MESH_ORIGINAL_NOT_FOUND");return MeshJson.copy(r);
    }
    @Override public synchronized void close()throws Exception{if(closed)return;closed=true;guard.close();}
}
