package com.lma.app.mesh;

import android.os.SystemClock;
import android.util.Base64;
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters;
import org.bouncycastle.crypto.signers.Ed25519Signer;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.*;

/** Existing P11 signed lease plus the separately versioned Android media manifest. */
public final class MeshLease {
    public static final String LEASE_PROFILE="P11_EXECUTION_LEASE_ED25519_1";
    public static final String MANIFEST_PROFILE="P11_ANDROID_MEDIA_MANIFEST_1";
    public final String id,job,attempt,order,seller,device,resource,capability,manifestHash,environment,metadataHash;
    public final String bundle,bundleHash,recipeHash,engine,engineBuild,backend,task,modelAlias,operation,requestPayloadHash;
    public static final class InputAsset {
        public final String id,role,payloadSha256;
        public final long bytes;
        private InputAsset(JSONObject value)throws Exception{
            MeshJson.fields(value,"asset_ref","role","payload_sha256","bytes");id=identifier(value,"asset_ref");role=MeshJson.string(value,"role");payloadSha256=digest(value,"payload_sha256");bytes=MeshJson.integer(value,"bytes",1,64L*1024*1024);
            if(!role.matches("[a-z][a-z0-9_]{0,31}"))throw new IOException("MESH_INPUT_ROLE");
        }
    }
    public final List<InputAsset> inputs;
    public final long generation,fence,inputBytes,maxWidth,maxHeight,maxFrames,maxSeconds;
    private final Instant expires,hard;
    private final long acceptedElapsed,remainingNanos;
    private final JSONObject manifest;
    private MeshLease(JSONObject lease,JSONObject m,byte[] original,Instant now) throws Exception {
        id=lease.getString("lease_id");job=lease.getString("job_id");attempt=lease.getString("attempt_id");order=lease.getString("order_id");
        seller=lease.getString("seller_id");device=lease.getString("device_id");resource=lease.getString("resource_id");capability=lease.getString("capability_id");
        generation=counter(lease,"resource_lock_generation");fence=counter(lease,"fencing_token");manifestHash=lease.getString("execution_manifest_hash");environment=lease.getString("kernel_environment_digest");metadataHash=MeshJournal.sha(original);
        bundle=m.getString("bundle_id");bundleHash=m.getString("bundle_sha256");recipeHash=m.getString("recipe_sha256");engine=m.getString("engine_id");engineBuild=m.getString("engine_build_sha256");backend=m.getString("backend");task=m.getString("task_type");modelAlias=m.getString("model_alias");operation=m.getString("operation");requestPayloadHash=m.getString("request_payload_sha256");
        List<InputAsset> inputList=new ArrayList<>();JSONArray inputAssets=m.optJSONArray("input_assets");if(inputAssets!=null)for(int i=0;i<inputAssets.length();i++)inputList.add(new InputAsset(inputAssets.getJSONObject(i)));inputs=Collections.unmodifiableList(inputList);
        inputBytes=MeshJson.integer(m,"input_bytes",1,64L*1024*1024);maxWidth=MeshJson.integer(m,"maximum_width",1,8192);maxHeight=MeshJson.integer(m,"maximum_height",1,8192);
        maxFrames=MeshJson.integer(m,"maximum_frames",1,4096);maxSeconds=MeshJson.integer(m,"maximum_seconds",1,3600);
        expires=instant(lease,"expires_at");hard=instant(lease,"hard_deadline");acceptedElapsed=SystemClock.elapsedRealtimeNanos();remainingNanos=Duration.between(now,expires).toNanos();manifest=MeshJson.copy(m);
    }
    /** Approval comes from the separately encrypted owner configuration, not a task response. */
    public static final class Approval {
        private final JSONObject values;
        public Approval(JSONObject localOwnerConfig) throws Exception {
            values=MeshJson.copy(localOwnerConfig);
            MeshJson.fields(values,"order_id","route_id","seller_id","device_id","resource_id","capability_id","contract_hash","kernel_environment_digest","bundle_id","bundle_sha256","recipe_sha256","engine_id","engine_build_sha256","backend","task_type","model_alias","operation","maximum_width","maximum_height","maximum_frames","maximum_seconds","mode","privacy");
            for(String name:new String[]{"order_id","route_id","seller_id","device_id","resource_id","capability_id","bundle_id","engine_id","model_alias"}) identifier(values,name);
            for(String name:new String[]{"contract_hash","kernel_environment_digest","bundle_sha256","recipe_sha256","engine_build_sha256"}) digest(values,name);
            if(!Arrays.asList("image.generate","video.generate").contains(values.getString("task_type"))||!Arrays.asList("cpu","vulkan").contains(values.getString("backend")))throw new IOException("MESH_APPROVAL_CAPABILITY");
            if(!Arrays.asList("OPENAI_IMAGES_GENERATIONS_V1","OPENAI_IMAGES_EDITS_V1","OPENAI_VIDEOS_V1").contains(values.getString("operation"))||values.getString("task_type").equals("video.generate")!=values.getString("operation").equals("OPENAI_VIDEOS_V1"))throw new IOException("MESH_APPROVAL_OPERATION");
            // Encrypted transport is a distinct subsequent protocol; this entry
            // must not advertise E2E/attestation based on app sandboxing alone.
            if(!"PLAINTEXT".equals(values.getString("mode"))||!"STANDARD".equals(values.getString("privacy")))throw new IOException("MESH_PRIVACY_PROFILE_NOT_IMPLEMENTED");
            MeshJson.integer(values,"maximum_width",1,8192);MeshJson.integer(values,"maximum_height",1,8192);MeshJson.integer(values,"maximum_frames",1,4096);MeshJson.integer(values,"maximum_seconds",1,3600);
        }
        void matches(JSONObject manifest) throws Exception {
            for(Iterator<String> i=values.keys();i.hasNext();) {
                String name=i.next();
                if(name.startsWith("maximum_")){
                    if(MeshJson.integer(manifest,name,1,Long.MAX_VALUE)>values.getLong(name))throw new IOException("MESH_OWNER_LIMIT");
                } else if(!MeshJson.string(values,name).equals(MeshJson.string(manifest,name)))throw new IOException("MESH_OWNER_APPROVAL_MISMATCH");
            }
        }
    }
    public static MeshLease verify(byte[] rawLease,byte[] rawManifest,byte[] independentIssuerPin,Approval independentApproval) throws Exception {
        if(independentIssuerPin==null||independentIssuerPin.length!=32||independentApproval==null)throw new IOException("MESH_INDEPENDENT_PIN_REQUIRED");
        JSONObject l=MeshJson.parse(rawLease,16384),m=MeshJson.parse(rawManifest,16384);
        MeshJson.fields(l,"lease_id","job_id","attempt_id","order_id","seller_id","device_id","resource_id","resource_lock_generation","capability_id","execution_manifest_ref","execution_manifest_hash","kernel_environment_digest","lease_revision","issued_at","not_before","hard_deadline","fencing_token","expires_at","contract_hash","input_asset_refs","envelope_ref","signature_profile_id","signature");
        MeshJson.fields(m,"profile_id","job_id","attempt_id","order_id","route_id","seller_id","device_id","resource_id","capability_id","resource_lock_generation","fencing_token","contract_hash","kernel_environment_digest","bundle_id","bundle_sha256","recipe_sha256","engine_id","engine_build_sha256","backend","task_type","model_alias","operation","maximum_width","maximum_height","maximum_frames","maximum_seconds","input_asset_ref","input_ciphertext_sha256","input_bytes","request_payload_sha256","input_assets","mode","privacy","hard_deadline");
        for(String name:new String[]{"lease_id","job_id","attempt_id","order_id","seller_id","device_id","resource_id","capability_id","execution_manifest_ref"})identifier(l,name);
        for(String name:new String[]{"execution_manifest_hash","kernel_environment_digest","contract_hash"})digest(l,name);
        for(String name:new String[]{"bundle_sha256","recipe_sha256","engine_build_sha256","input_ciphertext_sha256","request_payload_sha256"})digest(m,name);
        if(!LEASE_PROFILE.equals(l.getString("signature_profile_id"))||!MANIFEST_PROFILE.equals(m.getString("profile_id"))||!l.getString("execution_manifest_hash").equals(MeshJournal.sha(rawManifest)))throw new IOException("MESH_MANIFEST_DIGEST");
        counter(l,"resource_lock_generation");counter(l,"fencing_token");long revision=MeshJson.integer(l,"lease_revision",1,Integer.MAX_VALUE);
        List<String> fields=new ArrayList<>();Collections.addAll(fields,LEASE_PROFILE,l.getString("lease_id"),l.getString("job_id"),l.getString("attempt_id"),l.getString("order_id"),l.getString("seller_id"),l.getString("device_id"),l.getString("resource_id"),l.getString("resource_lock_generation"),l.getString("capability_id"),l.getString("execution_manifest_ref"),l.getString("execution_manifest_hash"),l.getString("kernel_environment_digest"),Long.toString(revision),l.getString("issued_at"),l.getString("not_before"),l.getString("hard_deadline"),l.getString("fencing_token"),l.getString("expires_at"),l.getString("contract_hash"));
        JSONArray assets=l.optJSONArray("input_asset_refs");
        if(l.has("input_asset_refs")&&assets==null)throw new IOException("MESH_ASSET_ARRAY");
        fields.add(Integer.toString(assets==null?0:assets.length()));Set<String> seen=new HashSet<>();
        if(assets!=null){if(assets.length()>50)throw new IOException("MESH_ASSET_LIMIT");for(int i=0;i<assets.length();i++){Object value=assets.get(i);if(!(value instanceof String)||!((String)value).matches("[A-Za-z][A-Za-z0-9_-]{2,127}")||!seen.add((String)value))throw new IOException("MESH_ASSET_ID");fields.add((String)value);}}
        String envelope=l.has("envelope_ref")?MeshJson.string(l,"envelope_ref"):"";
        if(!envelope.isEmpty()&&!envelope.matches("[A-Za-z][A-Za-z0-9_-]{2,127}"))throw new IOException("MESH_ENVELOPE_ID");fields.add(envelope);
        String signature=MeshJson.string(l,"signature");byte[] sig=Base64.decode(signature,Base64.URL_SAFE|Base64.NO_WRAP|Base64.NO_PADDING);
        if(sig.length!=64||!Base64.encodeToString(sig,Base64.URL_SAFE|Base64.NO_WRAP|Base64.NO_PADDING).equals(signature))throw new IOException("MESH_SIGNATURE_ENCODING");
        byte[] message=(String.join("\n",fields)+"\n").getBytes(StandardCharsets.US_ASCII);
        Ed25519Signer verifier=new Ed25519Signer();verifier.init(false,new Ed25519PublicKeyParameters(independentIssuerPin,0));verifier.update(message,0,message.length);
        if(!verifier.verifySignature(sig))throw new IOException("MESH_LEASE_SIGNATURE");
        Instant now=Instant.now(),issued=instant(l,"issued_at"),start=instant(l,"not_before"),end=instant(l,"expires_at"),hard=instant(l,"hard_deadline");
        if(start.isAfter(issued)||!issued.isBefore(end)||end.isAfter(hard)||Duration.between(issued,end).compareTo(Duration.ofSeconds(45))>0||now.isBefore(start)||!now.isBefore(end))throw new IOException("MESH_LEASE_TIME");
        for(String name:new String[]{"job_id","attempt_id","order_id","seller_id","device_id","resource_id","capability_id","resource_lock_generation","fencing_token","contract_hash","kernel_environment_digest","hard_deadline"})if(!MeshJson.string(l,name).equals(MeshJson.string(m,name)))throw new IOException("MESH_ORIGINAL_MANIFEST_OWNER");
        identifier(m,"input_asset_ref");if(!seen.contains(m.getString("input_asset_ref")))throw new IOException("MESH_ORIGINAL_INPUT_REFERENCE");
        JSONArray media=m.optJSONArray("input_assets");if(m.has("input_assets")&&media==null||media!=null&&media.length()>8)throw new IOException("MESH_INPUT_ASSET_LIMIT");
        Set<String> roles=new HashSet<>(),mediaIds=new HashSet<>();long mediaBytes=0;
        if(media!=null)for(int i=0;i<media.length();i++){
            InputAsset a=new InputAsset(media.getJSONObject(i));
            if(!seen.contains(a.id)||!roles.add(a.role)||!mediaIds.add(a.id)||a.id.equals(m.getString("input_asset_ref")))throw new IOException("MESH_ORIGINAL_INPUT_ASSET_BINDING");
            mediaBytes=Math.addExact(mediaBytes,a.bytes);if(mediaBytes>64L*1024*1024)throw new IOException("MESH_INPUT_ASSET_LIMIT");
        }
        independentApproval.matches(m);
        return new MeshLease(l,m,rawLease,now);
    }
    static String identifier(JSONObject o,String name)throws Exception{String s=MeshJson.string(o,name);if(!s.matches("[A-Za-z][A-Za-z0-9_-]{2,127}"))throw new IOException("MESH_ID");return s;}
    static String digest(JSONObject o,String name)throws Exception{String s=MeshJson.string(o,name);if(!s.matches("[a-f0-9]{64}"))throw new IOException("MESH_SHA256");return s;}
    static long counter(JSONObject o,String name)throws Exception{String s=MeshJson.string(o,name);if(!s.matches("[1-9][0-9]{0,18}"))throw new IOException("MESH_COUNTER");try{return Long.parseLong(s);}catch(NumberFormatException e){throw new IOException("MESH_COUNTER_EXHAUSTED");}}
    private static Instant instant(JSONObject o,String name)throws Exception{
        String value=MeshJson.string(o,name);
        if(!value.matches("(?:20[0-9]{2}|2100)-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\\.[0-9]{0,8}[1-9])?Z"))throw new IOException("MESH_UTC_INSTANT");
        return Instant.parse(value);
    }
    public void check()throws Exception{
        Instant now=Instant.now();long elapsed=SystemClock.elapsedRealtimeNanos()-acceptedElapsed;
        if(elapsed<0||elapsed>=remainingNanos||!now.isBefore(expires)||!now.isBefore(hard))throw new IOException("MESH_LEASE_STOP_REQUIRED");
    }
    public JSONObject safeManifest()throws Exception{return MeshJson.copy(manifest);}
}
