package com.lma.app;

import com.lma.app.mesh.*;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.*;
import java.security.MessageDigest;
import java.util.*;

/** Private original-lease input capabilities, not paths/URLs or public LMA history IDs. */
public final class MeshMediaAssets {
    public interface Source { InputStream openOriginal(String signedAssetId)throws Exception; }
    public interface Check { void run()throws Exception; }
    public interface Ownership { void opened(Closeable stream)throws Exception; void closed(Closeable stream); }
    private final Map<String,File> roles=new HashMap<>();
    private final JSONArray receipts=new JSONArray();
    public MeshMediaAssets(MeshLease lease,File privateWork,Source source,Check check,Ownership owner)throws Exception{
        if(!lease.inputs.isEmpty()&&source==null)throw new IOException("MESH_PRIVATE_ASSET_SOURCE_REQUIRED");
        byte[] buffer=new byte[65536];
        try{
            for(MeshLease.InputAsset asset:lease.inputs){
                check.run();File output=new File(privateWork,"input-"+UUID.randomUUID()+".bin");MessageDigest digest=MessageDigest.getInstance("SHA-256");long bytes=0;
                InputStream opened=source.openOriginal(asset.id);
                if(opened==null)throw new IOException("MESH_INPUT_STREAM_MISSING");
                try(InputStream input=opened;FileOutputStream target=new FileOutputStream(output)){
                    owner.opened(input);int n;
                    while((n=input.read(buffer))!=-1){check.run();bytes=Math.addExact(bytes,n);if(bytes>asset.bytes)throw new IOException("MESH_INPUT_BYTES");target.write(buffer,0,n);digest.update(buffer,0,n);}
                    target.getFD().sync();
                }finally{owner.closed(opened);}
                if(bytes!=asset.bytes)throw new IOException("MESH_INPUT_TRUNCATED");
                StringBuilder sha=new StringBuilder(64);for(byte b:digest.digest())sha.append(String.format(Locale.ROOT,"%02x",b&255));
                if(!sha.toString().equals(asset.payloadSha256))throw new IOException("MESH_INPUT_DIGEST");check.run();
                JSONObject actual=new MediaInspector().inspect(output);roles.put(asset.role,output);
                receipts.put(new JSONObject().put("role",asset.role).put("id",asset.id).put("bytes",bytes).put("metadata",actual));
            }
        }finally{Arrays.fill(buffer,(byte)0);}
    }
    public JSONArray receipts()throws Exception{return new JSONArray(receipts.toString());}
    public File file(String role)throws Exception{File file=roles.get(role);if(file==null||!file.isFile())throw new IOException("MESH_PRIVATE_INPUT_ROLE_MISSING");return file;}
}
