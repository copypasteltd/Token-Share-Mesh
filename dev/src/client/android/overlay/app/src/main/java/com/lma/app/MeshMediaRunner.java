package com.lma.app;

import com.lma.app.core.*;
import com.lma.app.mesh.*;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.nio.file.attribute.BasicFileAttributes;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;

/** Private C2C media execution. Does not write LMA JobStore, ArtifactStore or UI history. */
public final class MeshMediaRunner implements AutoCloseable {
    public interface Progress { void changed(String lease,String stage,Double fraction)throws Exception; }
    private final LmaApp app;
    private final EngineHost resident;
    private final MeshJournal journal;
    private final MeshSharePolicy policy;
    private final File workRoot,resultRoot;
    private final AtomicReference<Active> active=new AtomicReference<>();
    private volatile boolean closed;
    private final class Active {
        final MeshLease lease;final AtomicBoolean canceled=new AtomicBoolean();
        final List<Process> children=new ArrayList<>();
        final Set<Closeable> streams=new HashSet<>();
        Active(MeshLease lease){this.lease=lease;}
        void check()throws Exception {if(closed||canceled.get())throw new IOException("MESH_STOP_REQUIRED");lease.check();policy.check();}
        synchronized void child(Process process)throws Exception {
            if(process==null||children.size()>=128)throw new IOException("MESH_PROCESS_LIMIT");
            for(Process old:children)if(old!=process&&old.isAlive())throw new IOException("MESH_PREVIOUS_CHILD_NOT_STOPPED");
            children.add(process);check();
        }
        void stop()throws Exception {
            canceled.set(true);IOException problem=null;List<Closeable> inputHandles;List<Process> ownedChildren;
            synchronized(this){inputHandles=new ArrayList<>(streams);streams.clear();ownedChildren=new ArrayList<>(children);}
            for(Closeable stream:inputHandles)try{stream.close();}catch(IOException e){problem=e;}
            for(Process child:ownedChildren)try{ProcessControl.stop(child);}catch(IOException e){problem=e;}
            for(Process child:ownedChildren)if(child.isAlive())throw new IOException("MESH_CHILD_STOP_UNKNOWN");
            if(problem!=null)throw problem;
        }
    }
    /** Existing LMA foreground service supplies its real EngineHost and shared ExecutionGate. */
    public MeshMediaRunner(LmaApp app,EngineHost resident,MeshJournal journal,MeshSharePolicy policy)throws Exception{
        this.app=Objects.requireNonNull(app);this.resident=Objects.requireNonNull(resident);this.journal=Objects.requireNonNull(journal);this.policy=Objects.requireNonNull(policy);
        workRoot=privateDirectory(new File(app.getNoBackupFilesDir(),"share-mesh-work"));resultRoot=privateDirectory(new File(app.getNoBackupFilesDir(),"share-mesh-results"));
        if(journal.held())throw new IOException("MESH_RESTART_OWNER_QUARANTINED");
    }
    private static File privateDirectory(File root)throws Exception{
        File expected=new File(root.getAbsoluteFile().getParentFile().getCanonicalFile(),root.getName());
        if(Files.isSymbolicLink(root.toPath())||!root.getCanonicalFile().equals(expected))throw new IOException("MESH_PRIVATE_PATH");
        Files.createDirectories(root.toPath());root.setReadable(false,false);root.setWritable(false,false);root.setExecutable(false,false);root.setReadable(true,true);root.setWritable(true,true);root.setExecutable(true,true);return root;
    }
    /** Caller owns a verified/decrypted request and consumes the result key.
     * Neither this API nor a successful engine return grants charge authority.
     * Calls are synchronous on the provider's owned control worker; HTTP client
     * disconnects do not create implicit retry or a new local job. */
    public MeshResultCipher.Result execute(MeshLease lease,byte[] rawBody,byte[] resultKey,Progress progress)throws Exception{
        return execute(lease,rawBody,resultKey,null,progress);
    }
    public MeshResultCipher.Result execute(MeshLease lease,byte[] rawBody,byte[] resultKey,MeshMediaAssets.Source inputSource,Progress progress)throws Exception{
        Objects.requireNonNull(lease);Objects.requireNonNull(progress);
        if(resultKey==null||resultKey.length!=32)throw new IOException("MESH_RESULT_KEY_REQUIRED");
        Active run=new Active(lease);if(closed||!active.compareAndSet(null,run))throw new IOException("MESH_PHONE_SLOT_BUSY");
        byte[] ownedBody=rawBody==null?null:rawBody.clone(),ownedKey=resultKey.clone();Arrays.fill(resultKey,(byte)0);
        MeshJournal.Work work=null;ExecutionGate.Ticket slot=null;BundleStore.Selection selection=null;File directory=null;Set<Path> baseline=Collections.emptySet();
        ScheduledExecutorService watchdog=null;MeshResultCipher.Result encrypted=null;Exception failure=null;boolean clean=false,parked=false,baselineRecorded=false;
        try{
            run.check();new MeshLmaDescriptors(app).matches(lease);JSONObject request=MeshMediaRequests.normalize(app,lease,ownedBody);
            work=journal.admit(lease);slot=app.execution.enqueueIfIdle("mesh_media",lease.bundle);
            final ExecutionGate.Ticket owned=slot;owned.await(run::check);resident.park(owned);parked=true;run.check();policy.admit();
            directory=privateDirectory(new File(workRoot,UUID.randomUUID().toString()));final File privateWork=directory;
            baseline=workspaces();baselineRecorded=true;selection=app.bundles.acquireAll(Collections.singletonList(lease.bundle));new MeshLmaDescriptors(app).matches(lease);
            final BundleStore.Lease model=selection.models.get(0);LocalEngine engine=app.localEngines.require(model.bundle,lease.task);
            watchdog=Executors.newSingleThreadScheduledExecutor(r->{Thread t=new Thread(r,"Mesh-owned-stop");t.setDaemon(true);return t;});
            watchdog.scheduleWithFixedDelay(()->{try{run.check();}catch(Exception stop){try{run.stop();}catch(Exception unknown){/* Journal and execution slot remain held until the owning finally proves exit. */}}},200,200,TimeUnit.MILLISECONDS);
            final MeshMediaAssets privateInputs=new MeshMediaAssets(lease,privateWork,inputSource,run::check,new MeshMediaAssets.Ownership(){
                @Override public void opened(Closeable stream)throws Exception{synchronized(run){run.check();run.streams.add(stream);}}
                @Override public void closed(Closeable stream){synchronized(run){run.streams.remove(stream);}}
            });
            engine.inputs(model.bundle,lease.task).validate(privateInputs.receipts(),request.getJSONObject("input"));
            JSONObject frozen=engine.prepare(model.bundle,request);final JSONObject prepared=MeshJson.copy(frozen);
            for(String field:new String[]{"bundleId","type","input","parameters"})if(!MeshJson.canonical(prepared.get(field)).equals(MeshJson.canonical(request.get(field))))throw new IOException("MESH_FROZEN_REQUEST_CHANGED");
            journal.executing(work);
            File output=engine.generate(new LocalEngine.Context(){
                @Override public BundleStore.Lease model(){return model;}
                @Override public File input(String role)throws Exception{check();return privateInputs.file(role);}
                @Override public JSONObject request(){try{return MeshJson.copy(prepared);}catch(Exception e){throw new IllegalStateException("MESH_REQUEST_COPY");}}
                @Override public File output(String suffix)throws Exception{
                    check();if(!suffix.matches("[A-Za-z0-9][A-Za-z0-9_.-]{0,31}")||suffix.contains(".."))throw new IOException("MESH_OUTPUT_SUFFIX");
                    File file=new File(privateWork,UUID.randomUUID().toString()+"."+suffix);if(file.exists())throw new IOException("MESH_OUTPUT_COLLISION");return file;
                }
                @Override public void check()throws Exception{owned.checkOwner();run.check();}
                @Override public void progress(String stage,Double fraction)throws Exception{
                    check();String safe=stage!=null&&stage.matches("[a-z][a-z0-9_]{0,63}")?stage:"running";
                    if(fraction!=null&&(!Double.isFinite(fraction)||fraction<0||fraction>1))throw new IOException("MESH_PROGRESS_FRACTION");progress.changed(lease.id,safe,fraction);
                }
                @Override public void process(Process process)throws Exception{
                    try{run.child(process);owned.watchProcess(process);check();}catch(Exception e){ProcessControl.stop(process);throw e;}
                }
                @Override public void execution(JSONObject details)throws Exception{check();/* Raw worker details can contain paths. Never publish or persist them. */}
            });
            run.check();owned.checkOwner();
            if(output==null||!output.getCanonicalFile().getParentFile().equals(privateWork.getCanonicalFile())||Files.isSymbolicLink(output.toPath()))throw new IOException("MESH_OUTPUT_OWNER");
            JSONObject actual=new MediaInspector().inspect(output);validateOutput(lease,actual,prepared.getJSONObject("parameters"));
            encrypted=MeshResultCipher.encrypt(output,new File(resultRoot,UUID.randomUUID().toString().replace("-","")+".aead"),ownedKey,lease,()->{owned.checkOwner();run.check();});
            journal.result(work,encrypted);owned.finishResourceMonitoring();
        }catch(Exception error){failure=error;}
        finally{
            if(watchdog!=null){watchdog.shutdownNow();try{if(!watchdog.awaitTermination(8,TimeUnit.SECONDS))failure=new IOException("MESH_WATCHDOG_EXIT_UNKNOWN");}catch(InterruptedException e){Thread.currentThread().interrupt();failure=new IOException("MESH_WATCHDOG_INTERRUPTED");}}
            try{
                run.stop();if(work!=null)journal.stopping(work);
                if(directory!=null)removeOwnedTree(directory.toPath(),workRoot.toPath());
                if(directory!=null&&!baselineRecorded)throw new IOException("MESH_ENGINE_WORK_BASELINE_UNKNOWN");
                if(baselineRecorded)for(Path created:workspaces())if(!baseline.contains(created))removeOwnedTree(created,new File(app.getFilesDir(),"engine-work").toPath());
                if(selection!=null){selection.close();selection=null;}
                if(work!=null){
                    String cleanup="P11_ANDROID_CLEANUP_1\n"+lease.id+"\n"+lease.attempt+"\n"+lease.resource+"\n"+lease.generation+"\n"+lease.fence+"\n"+lease.metadataHash+"\nSTOPPED\nREVOKED\nCLEARED\n";
                    journal.cleaned(work,MeshJournal.sha(cleanup.getBytes(StandardCharsets.US_ASCII)),true,true,true);
                }
                clean=true;if(slot!=null)slot.close();if(parked)resident.resumeIfIdle();
            }catch(Exception unknown){
                if(work!=null)try{journal.quarantine(work);}catch(Exception journalUnknown){unknown.addSuppressed(journalUnknown);}
                failure=new IOException("MESH_CLEANUP_UNKNOWN_OWNER_RETAINED",unknown);
                // Do not release the physical gate or model references on an
                // unproven child/filesystem failure. Owner recovery is explicit.
            }
            if(failure!=null&&encrypted!=null&&encrypted.ciphertext.exists()){
                if(!encrypted.ciphertext.delete())failure=new IOException("MESH_RESULT_REVOCATION_PENDING",failure);
            }
            if(ownedBody!=null)Arrays.fill(ownedBody,(byte)0);Arrays.fill(ownedKey,(byte)0);
            if(clean)active.compareAndSet(run,null);
        }
        if(failure!=null)throw new IOException(safeCode(failure));
        if(encrypted==null)throw new IOException("MESH_RESULT_MISSING");return encrypted;
    }
    private static void validateOutput(MeshLease lease,JSONObject actual,JSONObject parameters)throws Exception{
        String expected=lease.task.equals("image.generate")?"image/png":"video/mp4";
        if(!expected.equals(actual.getString("mime"))||actual.getLong("width")!=parameters.getLong("width")||actual.getLong("height")!=parameters.getLong("height"))throw new IOException("MESH_OUTPUT_FORMAT_OR_SHAPE");
        if(lease.task.equals("video.generate")){
            long frames=MeshJson.integer(actual,"frames",1,lease.maxFrames),duration=MeshJson.integer(actual,"durationMs",1,Math.multiplyExact(lease.maxSeconds,1000)+1000);
            if(frames!=parameters.getLong("frames")||parameters.getLong("frames")>parameters.getLong("fps")*5&&duration<=5000)throw new IOException("MESH_OUTPUT_VIDEO_INCOMPLETE");
        }
    }
    private Set<Path> workspaces()throws Exception{
        Set<Path> paths=new HashSet<>();File root=new File(app.getFilesDir(),"engine-work");if(!root.exists())return paths;
        if(Files.isSymbolicLink(root.toPath()))throw new IOException("MESH_ENGINE_WORK_ROOT");File[] files=root.listFiles();if(files==null)throw new IOException("MESH_ENGINE_WORK_LIST");
        for(File f:files)if(f.getName().matches("work-[a-f0-9-]{36}"))paths.add(f.toPath().toAbsolutePath().normalize());return paths;
    }
    private static void removeOwnedTree(Path target,Path parent)throws Exception{
        Path root=parent.toRealPath(),absolute=target.toAbsolutePath().normalize();
        if(!absolute.getParent().equals(root)||Files.isSymbolicLink(absolute))throw new IOException("MESH_CLEANUP_PATH_OWNER");
        if(!Files.exists(absolute,LinkOption.NOFOLLOW_LINKS))return;
        Files.walkFileTree(absolute,new SimpleFileVisitor<Path>(){
            @Override public FileVisitResult visitFile(Path file,BasicFileAttributes attributes)throws IOException{Files.delete(file);return FileVisitResult.CONTINUE;}
            @Override public FileVisitResult postVisitDirectory(Path directory,IOException error)throws IOException{if(error!=null)throw error;Files.delete(directory);return FileVisitResult.CONTINUE;}
        });
    }
    private static String safeCode(Exception error){String text=error.getMessage();return text!=null&&text.matches("MESH_[A-Z0-9_]{1,96}")?text:"MESH_ENGINE_FAILED";}
    public void cancel(String originalLease)throws Exception{Active current=active.get();if(current==null||!current.lease.id.equals(originalLease))throw new IOException("MESH_ORIGINAL_CANCEL_OWNER");current.stop();}
    @Override public void close()throws Exception{closed=true;policy.ownerEnabled(false);Active current=active.get();if(current!=null)current.stop();}
}
