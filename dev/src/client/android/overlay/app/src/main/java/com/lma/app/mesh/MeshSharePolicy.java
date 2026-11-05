package com.lma.app.mesh;

import android.app.ActivityManager;
import android.app.KeyguardManager;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.os.BatteryManager;
import android.os.Build;
import android.os.PowerManager;
import java.io.IOException;
import org.json.JSONObject;

/** Explicit owner-selected idle-phone conditions, rechecked during execution. */
public final class MeshSharePolicy {
    private final Context app;
    private final boolean chargingOnly,wifiOnly,allowScreenOn;
    private final long reserveRam,estimatedRam,minimumDisk;
    private final int minimumBattery,maximumThermal;
    private volatile boolean accepting;
    public MeshSharePolicy(Context context,JSONObject owner)throws Exception{
        app=context.getApplicationContext();
        MeshJson.fields(owner,"charging_only","wifi_only","allow_screen_on","minimum_battery_percent","maximum_thermal_status","reserved_ram_bytes","estimated_peak_ram_bytes","minimum_free_disk_bytes");
        for(String k:new String[]{"charging_only","wifi_only","allow_screen_on"})if(!(owner.get(k)instanceof Boolean))throw new IOException("MESH_OWNER_BOOLEAN");
        chargingOnly=owner.getBoolean("charging_only");wifiOnly=owner.getBoolean("wifi_only");allowScreenOn=owner.getBoolean("allow_screen_on");
        minimumBattery=(int)MeshJson.integer(owner,"minimum_battery_percent",20,100);maximumThermal=(int)MeshJson.integer(owner,"maximum_thermal_status",0,3);
        reserveRam=MeshJson.integer(owner,"reserved_ram_bytes",512L*1024*1024,16L*1024*1024*1024);
        estimatedRam=MeshJson.integer(owner,"estimated_peak_ram_bytes",1,64L*1024*1024*1024);
        minimumDisk=MeshJson.integer(owner,"minimum_free_disk_bytes",128L*1024*1024,256L*1024*1024*1024);
    }
    /** Never implicitly enabled at boot, installation or receipt of a remote task. */
    public void ownerEnabled(boolean enabled){accepting=enabled;}
    public void check()throws Exception{check(false);}
    public void admit()throws Exception{check(true);}
    private void check(boolean admission)throws Exception{
        if(!accepting)throw new IOException("MESH_OWNER_SHARING_DISABLED");
        Intent battery=app.registerReceiver(null,new IntentFilter(Intent.ACTION_BATTERY_CHANGED));
        if(battery==null)throw new IOException("MESH_BATTERY_UNAVAILABLE");
        int scale=battery.getIntExtra(BatteryManager.EXTRA_SCALE,-1),level=battery.getIntExtra(BatteryManager.EXTRA_LEVEL,-1),plug=battery.getIntExtra(BatteryManager.EXTRA_PLUGGED,0);
        if(scale<=0||level<0||((long)level*100)/scale<minimumBattery||chargingOnly&&plug==0)throw new IOException("MESH_BATTERY_CONDITION");
        ConnectivityManager cm=(ConnectivityManager)app.getSystemService(Context.CONNECTIVITY_SERVICE);Network network=cm.getActiveNetwork();NetworkCapabilities capabilities=cm.getNetworkCapabilities(network);
        if(capabilities==null||!capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)||wifiOnly&&!capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI))throw new IOException("MESH_NETWORK_CONDITION");
        PowerManager power=(PowerManager)app.getSystemService(Context.POWER_SERVICE);
        if(!allowScreenOn&&power.isInteractive())throw new IOException("MESH_SCREEN_CONDITION");
        if(Build.VERSION.SDK_INT>=29&&power.getCurrentThermalStatus()>maximumThermal)throw new IOException("MESH_THERMAL_CONDITION");
        if(Build.VERSION.SDK_INT<29&&maximumThermal<3)throw new IOException("MESH_THERMAL_STATUS_UNAVAILABLE");
        ActivityManager.MemoryInfo memory=new ActivityManager.MemoryInfo();((ActivityManager)app.getSystemService(Context.ACTIVITY_SERVICE)).getMemoryInfo(memory);
        if(memory.lowMemory||memory.availMem<reserveRam||admission&&memory.availMem-reserveRam<estimatedRam)throw new IOException("MESH_MEMORY_CONDITION");
        if(app.getNoBackupFilesDir().getUsableSpace()<minimumDisk)throw new IOException("MESH_DISK_CONDITION");
    }
}
