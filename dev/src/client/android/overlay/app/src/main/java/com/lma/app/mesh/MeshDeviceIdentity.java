package com.lma.app.mesh;

import android.util.AtomicFile;
import android.util.Base64;
import org.bouncycastle.crypto.params.Ed25519PrivateKeyParameters;
import org.bouncycastle.crypto.signers.Ed25519Signer;
import org.json.JSONObject;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.SecureRandom;
import java.util.Arrays;
import java.util.UUID;

/** Local control identity. The platform receives only the public key, never the seed. */
public final class MeshDeviceIdentity implements AutoCloseable {
    private static final String PROFILE="P11_ANDROID_CONTROL_IDENTITY_1";
    private final MeshJournalGuard guard;
    private final byte[] seed;
    private boolean closed;
    public MeshDeviceIdentity(File noBackup,boolean initialize)throws Exception {
        File root=new File(noBackup,"share-mesh-identity");
        if(Files.isSymbolicLink(root.toPath())||!root.getCanonicalFile().equals(new File(noBackup.getCanonicalFile(),root.getName())))throw new IOException("MESH_IDENTITY_PATH");
        Files.createDirectories(root.toPath());guard=MeshJournalGuard.acquire(root);
        try{
            AtomicFile anchor=new AtomicFile(new File(root,"anchor.json")),secret=new AtomicFile(new File(root,"seed.aead"));
            JSONObject metadata;
            if(!anchor.getBaseFile().exists()){
                if(!initialize||secret.getBaseFile().exists()||new File(root,"seed.aead.bak").exists())throw new IOException("MESH_IDENTITY_NOT_INITIALIZED");
                String installation=UUID.randomUUID().toString().replace("-","");
                metadata=new JSONObject().put("profile",PROFILE).put("installation",installation);
                write(anchor,metadata.toString().getBytes(StandardCharsets.UTF_8));
                MeshKeystore wrapping=new MeshKeystore("p11_mesh_"+installation);wrapping.create();
                byte[] created=new byte[32];new SecureRandom().nextBytes(created);
                try{write(secret,wrapping.seal(created,aad(installation)));}finally{Arrays.fill(created,(byte)0);}
            }
            metadata=MeshJson.parse(read(anchor,4096),4096);MeshJson.fields(metadata,"profile","installation");
            String installation=MeshJson.string(metadata,"installation");
            if(!PROFILE.equals(metadata.getString("profile"))||!installation.matches("[a-f0-9]{32}"))throw new IOException("MESH_IDENTITY_ANCHOR");
            MeshKeystore wrapping=new MeshKeystore("p11_mesh_"+installation);
            if(!wrapping.exists())throw new IOException("MESH_IDENTITY_KEY_LOST");
            seed=wrapping.open(read(secret,256),aad(installation));
            if(seed.length!=32){Arrays.fill(seed,(byte)0);throw new IOException("MESH_IDENTITY_SEED");}
        }catch(Exception error){guard.close();throw error;}
    }
    private static byte[] aad(String installation){return(PROFILE+"\n"+installation+"\n").getBytes(StandardCharsets.US_ASCII);}
    public synchronized String publicKey()throws Exception{
        check();return encoded(new Ed25519PrivateKeyParameters(seed,0).generatePublicKey().getEncoded());
    }
    public synchronized String sign(byte[] message)throws Exception{
        check();if(message==null||message.length>131072)throw new IOException("MESH_SIGNATURE_LIMIT");
        Ed25519Signer signer=new Ed25519Signer();signer.init(true,new Ed25519PrivateKeyParameters(seed,0));signer.update(message,0,message.length);return encoded(signer.generateSignature());
    }
    public static String encoded(byte[] bytes){return Base64.encodeToString(bytes,Base64.URL_SAFE|Base64.NO_WRAP|Base64.NO_PADDING);}
    public static byte[] decoded(String value,int bytes)throws Exception{
        byte[] decoded=Base64.decode(value,Base64.URL_SAFE|Base64.NO_WRAP|Base64.NO_PADDING);
        if(decoded.length!=bytes||!encoded(decoded).equals(value))throw new IOException("MESH_BASE64URL_PROFILE");return decoded;
    }
    private void check()throws IOException{if(closed)throw new IOException("MESH_IDENTITY_CLOSED");}
    static byte[] read(AtomicFile file,int maximum)throws Exception{
        try(InputStream input=file.openRead();ByteArrayOutputStream out=new ByteArrayOutputStream()){
            byte[] buffer=new byte[4096];int count;
            while((count=input.read(buffer))!=-1){if(out.size()+count>maximum)throw new IOException("MESH_PRIVATE_RECORD_LIMIT");out.write(buffer,0,count);}return out.toByteArray();
        }
    }
    static void write(AtomicFile file,byte[] value)throws Exception{
        FileOutputStream output=file.startWrite();try{output.write(value);file.finishWrite(output);}catch(Exception error){file.failWrite(output);throw error;}
    }
    @Override public synchronized void close()throws Exception{if(closed)return;closed=true;Arrays.fill(seed,(byte)0);guard.close();}
}
