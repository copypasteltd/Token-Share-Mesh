package com.lma.app.mesh;

import java.io.*;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.Arrays;
import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

/** Bounded streaming ciphertext; no Base64 video or full-output RAM copy. */
public final class MeshResultCipher {
    private static final int CHUNK=65536;
    public interface Check { void run() throws Exception; }
    public static final class Result {
        public final File ciphertext;
        public final String ciphertextSha256;
        public final long ciphertextBytes,plaintextBytes;
        private Result(File file,String sha,long cipher,long plain){ciphertext=file;ciphertextSha256=sha;ciphertextBytes=cipher;plaintextBytes=plain;}
    }
    private MeshResultCipher(){}
    public static Result encrypt(File plaintext,File destination,byte[] recipientContentKey,MeshLease lease,Check check)throws Exception {
        if(recipientContentKey==null||recipientContentKey.length!=32||Files.isSymbolicLink(plaintext.toPath())||!plaintext.isFile()||destination.exists())throw new IOException("MESH_RESULT_INPUT");
        long size=plaintext.length();if(size<1||size>64L*1024*1024-65536)throw new IOException("MESH_RESULT_LIMIT");
        byte[] key=recipientContentKey.clone(),noncePrefix=new byte[8],buffer=new byte[CHUNK];new SecureRandom().nextBytes(noncePrefix);
        byte[] context=("P11_ANDROID_RESULT_AESGCM_1\n"+lease.id+"\n"+lease.attempt+"\n"+lease.resource+"\n"+lease.generation+"\n"+lease.fence+"\n"+lease.manifestHash+"\n").getBytes(StandardCharsets.US_ASCII);
        MessageDigest digest=MessageDigest.getInstance("SHA-256");boolean committed=false;long written=0,consumed=0;
        try(InputStream input=new FileInputStream(plaintext);FileOutputStream output=new FileOutputStream(destination)){
            byte[] header=ByteBuffer.allocate(32).put("MESHRES1".getBytes(StandardCharsets.US_ASCII)).putInt(CHUNK).putLong(size).put(noncePrefix).putInt((int)((size+CHUNK-1)/CHUNK)).array();
            output.write(header);digest.update(header);written+=header.length;
            int sequence=0;
            while(consumed<size){
                check.run();int wanted=(int)Math.min(CHUNK,size-consumed),count=0;
                while(count<wanted){int n=input.read(buffer,count,wanted-count);if(n<0)throw new IOException("MESH_RESULT_TRUNCATED");count+=n;}
                boolean last=consumed+count==size;byte[] index=ByteBuffer.allocate(17).putInt(sequence).putInt(count).put((byte)(last?1:0)).putLong(size).array();
                byte[] nonce=ByteBuffer.allocate(12).put(noncePrefix).putInt(sequence).array();
                Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");cipher.init(Cipher.ENCRYPT_MODE,new SecretKeySpec(key,"AES"),new GCMParameterSpec(128,nonce));cipher.updateAAD(context);cipher.updateAAD(index);
                byte[] encrypted=cipher.doFinal(buffer,0,count);output.write(index);output.write(encrypted);digest.update(index);digest.update(encrypted);written+=index.length+encrypted.length;
                Arrays.fill(encrypted,(byte)0);Arrays.fill(buffer,0,count,(byte)0);consumed+=count;sequence++;
            }
            if(input.read()!=-1||plaintext.length()!=size)throw new IOException("MESH_RESULT_CHANGED");check.run();output.getFD().sync();committed=true;
        }finally{
            Arrays.fill(key,(byte)0);Arrays.fill(buffer,(byte)0);
            if(!committed&&destination.exists()&&!destination.delete())throw new IOException("MESH_INCOMPLETE_CIPHERTEXT_CLEANUP");
        }
        StringBuilder sha=new StringBuilder(64);for(byte value:digest.digest())sha.append(String.format(java.util.Locale.ROOT,"%02x",value&255));
        return new Result(destination,sha.toString(),written,size);
    }
}
