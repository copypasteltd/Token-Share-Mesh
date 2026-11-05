package com.lma.app.mesh;

import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import java.io.IOException;
import java.security.KeyStore;
import java.security.SecureRandom;
import java.util.Arrays;
import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/** App UID scoped Android Keystore master; no export of the wrapping key. */
public final class MeshKeystore {
    private final String alias;
    public MeshKeystore(String alias) throws Exception {
        if (!alias.matches("p11_mesh_[a-f0-9]{32}")) throw new IOException("MESH_KEY_ALIAS");
        this.alias = alias;
    }
    private KeyStore store() throws Exception { KeyStore s=KeyStore.getInstance("AndroidKeyStore");s.load(null);return s; }
    public boolean exists() throws Exception { return store().containsAlias(alias); }
    public void create() throws Exception {
        if (exists()) throw new IOException("MESH_KEY_ALREADY_EXISTS");
        KeyGenerator generator=KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES,"AndroidKeyStore");
        generator.init(new KeyGenParameterSpec.Builder(alias,KeyProperties.PURPOSE_ENCRYPT|KeyProperties.PURPOSE_DECRYPT)
                .setKeySize(256).setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setRandomizedEncryptionRequired(true).setUserAuthenticationRequired(false).build());
        generator.generateKey();
    }
    private SecretKey key() throws Exception {
        java.security.Key k=store().getKey(alias,null);
        if (!(k instanceof SecretKey)) throw new IOException("MESH_KEY_MISSING_OR_LOCKED");
        return (SecretKey)k;
    }
    public byte[] seal(byte[] input,byte[] aad) throws Exception {
        Cipher c=Cipher.getInstance("AES/GCM/NoPadding");c.init(Cipher.ENCRYPT_MODE,key());c.updateAAD(aad);
        byte[] iv=c.getIV(),cipher=c.doFinal(input);
        if (iv.length!=12) throw new IOException("MESH_IV_PROFILE");
        byte[] result=new byte[12+cipher.length];System.arraycopy(iv,0,result,0,12);System.arraycopy(cipher,0,result,12,cipher.length);return result;
    }
    public byte[] open(byte[] input,byte[] aad) throws Exception {
        if (input.length<28||input.length>131100) throw new IOException("MESH_SEALED_LIMIT");
        Cipher c=Cipher.getInstance("AES/GCM/NoPadding");c.init(Cipher.DECRYPT_MODE,key(),new GCMParameterSpec(128,Arrays.copyOf(input,12)));c.updateAAD(aad);
        return c.doFinal(input,12,input.length-12);
    }
}
