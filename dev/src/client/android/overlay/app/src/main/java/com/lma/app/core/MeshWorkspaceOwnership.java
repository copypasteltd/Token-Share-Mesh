package com.lma.app.core;

import java.io.File;
import java.io.IOException;

/** Synchronous pre-create hook; normal LMA jobs have no observer and keep their existing behavior. */
public final class MeshWorkspaceOwnership implements AutoCloseable {
    public interface Observer { void beforeCreate(File planned)throws Exception; }
    private static final ThreadLocal<Observer> CURRENT=new ThreadLocal<>();
    private final Thread owner;
    private boolean closed;
    private MeshWorkspaceOwnership(Observer observer)throws Exception{
        if(observer==null||CURRENT.get()!=null)throw new IOException("MESH_WORKSPACE_SCOPE");owner=Thread.currentThread();CURRENT.set(observer);
    }
    public static MeshWorkspaceOwnership bind(Observer observer)throws Exception{return new MeshWorkspaceOwnership(observer);}
    public static void beforeCreate(File planned)throws Exception{Observer observer=CURRENT.get();if(observer!=null)observer.beforeCreate(planned);}
    @Override public void close()throws IOException{
        if(closed)return;if(Thread.currentThread()!=owner)throw new IOException("MESH_WORKSPACE_SCOPE_THREAD");closed=true;CURRENT.remove();
    }
}
