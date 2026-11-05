package com.lma.app.mesh;

import java.io.IOException;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicReference;

/** One bounded metadata slot; a slow control connection never blocks the native output reader. */
public final class MeshProgressPump implements AutoCloseable {
    public interface Stop { void originalLease()throws Exception; }
    private static final class Value {
        final Double fraction;
        Value(Double fraction){this.fraction=fraction;}
    }
    private final MeshLease original;
    private final MeshControlReporter reports;
    private final MeshControlClient connection;
    private final Stop stop;
    private final AtomicReference<Value> pending=new AtomicReference<>();
    private final AtomicReference<IOException> failure=new AtomicReference<>();
    private final ScheduledExecutorService network;
    private volatile boolean closing;
    public MeshProgressPump(MeshLease original,MeshControlReporter reports,MeshControlClient connection,Stop stop){
        this.original=original;this.reports=reports;this.connection=connection;this.stop=stop;
        network=Executors.newSingleThreadScheduledExecutor(r->{Thread thread=new Thread(r,"Mesh-progress-metadata");thread.setDaemon(true);return thread;});
        network.scheduleWithFixedDelay(this::flush,0,2,TimeUnit.SECONDS);
    }
    public void changed(Double fraction)throws Exception{
        check();original.check();if(fraction!=null&&(!Double.isFinite(fraction)||fraction<0||fraction>1))throw new IOException("MESH_PROGRESS_FRACTION");pending.set(new Value(fraction));
    }
    private void flush(){
        if(closing||failure.get()!=null)return;Value value=pending.getAndSet(null);if(value==null)return;
        try{reports.progress(original,"RUNNING",value.fraction);}catch(Exception error){
            if(closing)return;String code=error.getMessage();IOException safe=new IOException(code!=null&&code.matches("MESH_[A-Z0-9_]{1,96}")?code:"MESH_PROGRESS_UNACKNOWLEDGED");
            if(failure.compareAndSet(null,safe))try{stop.originalLease();}catch(Exception unknown){failure.set(new IOException("MESH_PROGRESS_STOP_UNKNOWN"));}
        }
    }
    public void check()throws IOException{IOException current=failure.get();if(current!=null)throw current;if(closing)throw new IOException("MESH_PROGRESS_CLOSED");}
    @Override public void close()throws Exception{
        if(closing)return;closing=true;pending.set(null);network.shutdownNow();connection.cancelInflight();
        try{if(!network.awaitTermination(12,TimeUnit.SECONDS))throw new IOException("MESH_PROGRESS_WORKER_STOP_UNKNOWN");}catch(InterruptedException interrupted){Thread.currentThread().interrupt();throw new IOException("MESH_PROGRESS_STOP_INTERRUPTED");}
        IOException current=failure.get();if(current!=null)throw current;
    }
}
