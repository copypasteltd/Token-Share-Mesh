package com.lma.app;

import android.os.Binder;
import android.os.Process;

/** The service remains non-exported; no cross-UID control or customer payload Intent extras. */
public final class MeshServiceBinder extends Binder {
    private final MeshProviderController controller;
    public MeshServiceBinder(MeshProviderController controller){this.controller=controller;}
    public MeshProviderController controller(){if(Binder.getCallingUid()!=Process.myUid())throw new SecurityException("MESH_LOCAL_UID_REQUIRED");return controller;}
}
