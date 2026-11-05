package com.lma.app;

import android.provider.Settings;
import com.lma.app.core.ExecutionGate;
import com.lma.app.mesh.*;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;

/** Restart recovery requires a different Android boot, not a guessed PID or a forced FREE flag. */
public final class MeshMediaRecovery {
    private final LmaApp app;
    private final EngineHost resident;
    private final MeshJournal journal;
    private ExecutionGate.Ticket retainedSlot;
    private boolean parked;
    public MeshMediaRecovery(LmaApp app,EngineHost resident,MeshJournal journal){this.app=app;this.resident=resident;this.journal=journal;}
    public synchronized void afterReboot()throws Exception{
        MeshJournal.Work work=journal.recoverable();JSONObject facts=journal.recoveryData(work);
        int boot=Settings.Global.getInt(app.getContentResolver(),Settings.Global.BOOT_COUNT,-1);
        if(boot<0||boot==facts.getInt("boot_count"))throw new IOException("MESH_RECOVERY_REBOOT_REQUIRED");
        if(boot<facts.getInt("boot_count"))throw new IOException("MESH_RECOVERY_BOOT_ROLLBACK");
        if(retainedSlot==null)retainedSlot=app.execution.enqueueIfIdle("mesh_recovery","");
        ExecutionGate.Ticket slot=retainedSlot;if(slot==null)throw new IOException("MESH_PHONE_SLOT_BUSY");boolean released=false;
        try{
            slot.await(()->{});if(!parked){resident.park(slot);parked=true;}slot.checkOwner();
            String directory=facts.getString("private_directory");
            if(!directory.isEmpty()){if(!directory.matches("[a-f0-9-]{36}"))throw new IOException("MESH_RECOVERY_DIRECTORY");File parent=new File(app.getNoBackupFilesDir(),"share-mesh-work");MeshMediaRunner.removeOwnedTree(new File(parent,directory).toPath(),parent.toPath());}
            JSONArray paths=facts.getJSONArray("engine_workspaces");
            for(int i=0;i<paths.length();i++){String path=paths.getString(i);if(!path.matches("work-[a-f0-9-]{36}"))throw new IOException("MESH_RECOVERY_WORKSPACE");File parent=new File(app.getFilesDir(),"engine-work");MeshMediaRunner.removeOwnedTree(new File(parent,path).toPath(),parent.toPath());}
            JSONObject result=facts.optJSONObject("ciphertext");
            if(result!=null){String name=result.getString("file");if(!name.matches("[a-f0-9]{32}\\.aead"))throw new IOException("MESH_RECOVERY_RESULT");File parent=new File(app.getNoBackupFilesDir(),"share-mesh-results"),file=new File(parent,name);
                if(Files.isSymbolicLink(parent.toPath())||Files.isSymbolicLink(file.toPath())||!file.getCanonicalFile().getParentFile().equals(parent.getCanonicalFile()))throw new IOException("MESH_RECOVERY_RESULT_PATH");Files.deleteIfExists(file.toPath());}
            journal.stopping(work);
            String proof="P11_ANDROID_REBOOT_CLEANUP_1\n"+work.lease+"\n"+work.attempt+"\n"+work.resource+"\n"+work.generation+"\n"+work.fence+"\n"+work.metadataHash+"\n"+facts.getInt("boot_count")+"\n"+boot+"\nSTOPPED\nREVOKED\nCLEARED\n";
            journal.cleaned(work,MeshJournal.sha(proof.getBytes(StandardCharsets.US_ASCII)),true,true,true);released=true;
        }finally{if(released){slot.close();retainedSlot=null;boolean resume=parked;parked=false;if(resume)resident.resumeIfIdle();}else{try{journal.quarantine(work);}catch(Exception unknown){/* Retain this recovery object's original slot for an explicit retry. */}}}
    }
}
