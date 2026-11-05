package com.lma.app.mesh;

import org.json.JSONObject;
import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.*;

/** Existing P11 control proof over exact request bytes. No content listener or implicit retry. */
public final class MeshControlClient implements AutoCloseable {
    public static final class Reply {
        public final int status;public final byte[] body;
        private Reply(int status,byte[] body){this.status=status;this.body=body;}
    }
    private final URI origin;
    private final MeshDeviceIdentity identity;
    private final String device;
    private final Set<HttpURLConnection> connections=new HashSet<>();
    private volatile boolean closed;
    public MeshControlClient(JSONObject config,MeshDeviceIdentity identity)throws Exception{
        this.identity=Objects.requireNonNull(identity);origin=MeshOwnerConfig.origin(config.getString("control_origin"),config.getBoolean("allow_development_http"));
        device=MeshLease.identifier(config.getJSONObject("approval"),"device_id");
        if(!identity.publicKey().equals(config.getString("device_public_key")))throw new IOException("MESH_CONTROL_IDENTITY");
    }
    public Reply request(String method,String path,String key,byte[] exactBody,int maximumReply)throws Exception{
        if(!Arrays.asList("GET","POST","PATCH","DELETE").contains(method)||path==null||!path.matches("/v1/p11/[A-Za-z0-9_/-]+")||path.contains("//")||path.endsWith("/")||key==null||!key.matches("[A-Za-z0-9_-]{0,128}")||maximumReply<1||maximumReply>256*1024)throw new IOException("MESH_CONTROL_REQUEST");
        byte[] body=exactBody==null?new byte[0]:exactBody.clone();HttpURLConnection connection=null;
        try{
            if(body.length>65536||"GET".equals(method)&&body.length!=0)throw new IOException("MESH_CONTROL_BODY_LIMIT");
            byte[] nonce=new byte[16];new SecureRandom().nextBytes(nonce);String random=MeshDeviceIdentity.encoded(nonce),timestamp=Long.toString(Instant.now().getEpochSecond());
            String contentType=body.length==0?"":"application/json";
            String message="P11_DEVICE_ED25519_1\n"+device+"\n"+timestamp+"\n"+random+"\n"+method+"\n"+origin.getRawAuthority()+"\n"+path+"\n"+key+"\n\n"+contentType+"\n"+MeshJournal.sha(body)+"\n";
            String proof="p11d."+device+"."+timestamp+"."+random+"."+identity.sign(message.getBytes(StandardCharsets.US_ASCII));
            connection=(HttpURLConnection)origin.resolve(path).toURL().openConnection();
            synchronized(connections){if(closed){connection.disconnect();throw new IOException("MESH_CONTROL_CLOSED");}connections.add(connection);}
            connection.setInstanceFollowRedirects(false);connection.setUseCaches(false);connection.setConnectTimeout(10000);connection.setReadTimeout(10000);connection.setRequestMethod(method);
            connection.setRequestProperty("Authorization","Bearer "+proof);connection.setRequestProperty("Accept","application/json");connection.setRequestProperty("Accept-Encoding","identity");
            if(!key.isEmpty())connection.setRequestProperty("Idempotency-Key",key);
            if(body.length!=0){connection.setDoOutput(true);connection.setFixedLengthStreamingMode(body.length);connection.setRequestProperty("Content-Type",contentType);try(OutputStream out=connection.getOutputStream()){out.write(body);}}
            int status=connection.getResponseCode();if(status>=300&&status<400)throw new IOException("MESH_CONTROL_REDIRECT_REJECTED");
            long declared=connection.getContentLengthLong();if(declared>maximumReply)throw new IOException("MESH_CONTROL_REPLY_LIMIT");
            InputStream source=status>=400?connection.getErrorStream():connection.getInputStream();
            if(source==null)return new Reply(status,new byte[0]);
            try(InputStream input=source;ByteArrayOutputStream out=new ByteArrayOutputStream()){
                byte[] buffer=new byte[4096];int count;
                while((count=input.read(buffer))!=-1){if(out.size()+count>maximumReply)throw new IOException("MESH_CONTROL_REPLY_LIMIT");out.write(buffer,0,count);}
                return new Reply(status,out.toByteArray());
            }
        }finally{Arrays.fill(body,(byte)0);if(connection!=null){synchronized(connections){connections.remove(connection);}connection.disconnect();}}
    }
    /** Interrupt only currently owned I/O. The next original cleanup retry may still use this client. */
    public void cancelInflight(){List<HttpURLConnection> owned;synchronized(connections){owned=new ArrayList<>(connections);}for(HttpURLConnection connection:owned)connection.disconnect();}
    @Override public void close(){closed=true;cancelInflight();}
}
