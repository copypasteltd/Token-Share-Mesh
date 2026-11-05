package com.lma.app.mesh;

import android.util.AtomicFile;
import org.json.JSONObject;
import java.io.*;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.Arrays;
import java.util.UUID;

/** Explicitly imported owner approval; tasks cannot rewrite pins, policy or kernel choices. */
public final class MeshOwnerConfig implements AutoCloseable {
    public static final String PROFILE="P11_ANDROID_OWNER_CONFIG_1";
    private final File root;
    private final MeshJournalGuard guard;
    private final AtomicFile config;
    private final MeshKeystore wrapping;
    private final byte[] aad;
    private boolean closed;
    public MeshOwnerConfig(File noBackup,boolean initialize)throws Exception{
        root=new File(noBackup,"share-mesh-config");
        if(Files.isSymbolicLink(root.toPath())||!root.getCanonicalFile().equals(new File(noBackup.getCanonicalFile(),root.getName())))throw new IOException("MESH_CONFIG_PATH");
        Files.createDirectories(root.toPath());guard=MeshJournalGuard.acquire(root);
        try{
            AtomicFile anchor=new AtomicFile(new File(root,"anchor.json"));config=new AtomicFile(new File(root,"config.aead"));JSONObject metadata;
            if(!anchor.getBaseFile().exists()){
                if(!initialize||config.getBaseFile().exists()||new File(root,"config.aead.bak").exists())throw new IOException("MESH_CONFIG_NOT_INITIALIZED");
                String installation=UUID.randomUUID().toString().replace("-","");metadata=new JSONObject().put("profile",PROFILE).put("installation",installation);
                MeshDeviceIdentity.write(anchor,metadata.toString().getBytes(StandardCharsets.UTF_8));new MeshKeystore("p11_mesh_"+installation).create();
            }
            metadata=MeshJson.parse(MeshDeviceIdentity.read(anchor,4096),4096);MeshJson.fields(metadata,"profile","installation");
            String installation=MeshJson.string(metadata,"installation");
            if(!PROFILE.equals(metadata.getString("profile"))||!installation.matches("[a-f0-9]{32}"))throw new IOException("MESH_CONFIG_ANCHOR");
            wrapping=new MeshKeystore("p11_mesh_"+installation);if(!wrapping.exists())throw new IOException("MESH_CONFIG_KEY_LOST");
            aad=(PROFILE+"\n"+installation+"\n").getBytes(StandardCharsets.US_ASCII);
        }catch(Exception error){guard.close();throw error;}
    }
    public synchronized boolean exists(){return config.getBaseFile().exists()||new File(root,"config.aead.bak").exists();}
    public synchronized JSONObject load(String localPublicKey)throws Exception{
        check();byte[] clear=wrapping.open(MeshDeviceIdentity.read(config,65564),aad);
        try{return validate(MeshJson.parse(clear,65536),localPublicKey);}finally{Arrays.fill(clear,(byte)0);}
    }
    public synchronized void importApproved(byte[] raw,String localPublicKey)throws Exception{
        check();JSONObject values=validate(MeshJson.parse(raw,65536),localPublicKey);byte[] clear=values.toString().getBytes(StandardCharsets.UTF_8);
        try{MeshDeviceIdentity.write(config,wrapping.seal(clear,aad));}finally{Arrays.fill(clear,(byte)0);}
    }
    /** Control receipts only; the reporter validates their exact bounded schema before storage. */
    public synchronized JSONObject controlRecords()throws Exception{
        check();AtomicFile file=new AtomicFile(new File(root,"control.aead"));
        if(!file.getBaseFile().exists()&&!new File(root,"control.aead.bak").exists())return new JSONObject().put("profile","P11_ANDROID_CONTROL_OUTBOX_1").put("rows",new JSONObject()).put("sequences",new JSONObject());
        byte[] clear=wrapping.open(MeshDeviceIdentity.read(file,65564),controlAad());
        try{return MeshJson.parse(clear,65536);}finally{Arrays.fill(clear,(byte)0);}
    }
    public synchronized void controlRecords(JSONObject metadata)throws Exception{
        check();byte[] clear=metadata.toString().getBytes(StandardCharsets.UTF_8);
        try{if(clear.length>65536)throw new IOException("MESH_CONTROL_HISTORY_LIMIT");MeshDeviceIdentity.write(new AtomicFile(new File(root,"control.aead")),wrapping.seal(clear,controlAad()));}finally{Arrays.fill(clear,(byte)0);}
    }
    private byte[] controlAad(){byte[] suffix="CONTROL_OUTBOX\n".getBytes(StandardCharsets.US_ASCII),bound=Arrays.copyOf(aad,aad.length+suffix.length);System.arraycopy(suffix,0,bound,aad.length,suffix.length);return bound;}
    public static JSONObject validate(JSONObject values,String localPublicKey)throws Exception{
        MeshJson.fields(values,"profile","control_origin","allow_development_http","issuer_public_key","device_public_key","device_version","approval","conditions");
        if(!PROFILE.equals(MeshJson.string(values,"profile"))||!localPublicKey.equals(MeshJson.string(values,"device_public_key")))throw new IOException("MESH_CONFIG_IDENTITY_BINDING");
        MeshDeviceIdentity.decoded(MeshJson.string(values,"issuer_public_key"),32);MeshDeviceIdentity.decoded(localPublicKey,32);
        MeshJson.integer(values,"device_version",1,Integer.MAX_VALUE-1);
        if(!(values.get("allow_development_http")instanceof Boolean))throw new IOException("MESH_CONFIG_HTTP_CHOICE");
        origin(MeshJson.string(values,"control_origin"),values.getBoolean("allow_development_http"));
        new MeshLease.Approval(values.getJSONObject("approval"));
        // Constructing a policy requires Android services; its strict value validation occurs before enabling.
        if(!(values.get("conditions")instanceof JSONObject))throw new IOException("MESH_CONFIG_CONDITIONS");
        return MeshJson.copy(values);
    }
    public static URI origin(String value,boolean development)throws Exception{
        if(value==null||value.length()<1||value.length()>2048)throw new IOException("MESH_CONTROL_ORIGIN");
        URI uri=new URI(value);
        if(uri.getHost()==null||uri.getRawUserInfo()!=null||uri.getRawQuery()!=null||uri.getRawFragment()!=null||!"".equals(uri.getRawPath())||uri.getPort()==0||uri.getPort()>65535||!uri.toASCIIString().equals(value))throw new IOException("MESH_CONTROL_ORIGIN");
        if(!"https".equals(uri.getScheme())&&!(development&&"http".equals(uri.getScheme())&&privateNumeric(uri.getHost())))throw new IOException("MESH_CONTROL_ORIGIN_SCHEME");
        return uri;
    }
    private static boolean privateNumeric(String host){
        if("[::1]".equals(host)||"::1".equals(host))return true;
        if(!host.matches("(?:[0-9]{1,3}\\.){3}[0-9]{1,3}"))return false;
        String[] parts=host.split("\\.");int[] octets=new int[4];for(int i=0;i<4;i++){octets[i]=Integer.parseInt(parts[i]);if(octets[i]>255||!Integer.toString(octets[i]).equals(parts[i]))return false;}
        return octets[0]==127||octets[0]==10||octets[0]==192&&octets[1]==168||octets[0]==172&&octets[1]>=16&&octets[1]<=31;
    }
    private void check()throws IOException{if(closed)throw new IOException("MESH_CONFIG_CLOSED");}
    @Override public synchronized void close()throws Exception{if(closed)return;closed=true;guard.close();}
}
