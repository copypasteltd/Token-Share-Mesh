package com.lma.app;

import com.lma.app.core.ModelBundle;
import com.lma.app.core.DataFiles;
import com.lma.app.mesh.MeshJson;
import com.lma.app.mesh.MeshJournal;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.*;
import java.nio.charset.StandardCharsets;

/** Actual installed modules and explicit model/component/recipe identity; no catalogue-as-ready claim. */
public final class MeshLmaDescriptors {
    private final LmaApp app;
    public MeshLmaDescriptors(LmaApp app){this.app=app;}
    public JSONObject describe(String bundleId,String type)throws Exception{
        ModelBundle bundle=app.bundles.bundle(bundleId);
        if(!app.bundles.plan(bundleId).getBoolean("installed"))throw new IOException("MESH_MODEL_COMPONENTS_MISSING");
        LocalEngine engine=app.localEngines.require(bundle,type);
        JSONObject builds;
        try(InputStream input=app.getAssets().open("engine-builds.json")){builds=new JSONObject(Store.read(input,512*1024));}
        JSONArray list=builds.getJSONArray("engines");JSONObject build=null;
        for(int i=0;i<list.length();i++)if(list.getJSONObject(i).getString("engine").equals(bundle.engine)){build=list.getJSONObject(i);break;}
        if(build==null)throw new IOException("MESH_ENGINE_BUILD_MISSING");
        checkBinary(build);
        JSONArray dependencies=build.optJSONArray("dependencies");
        if(dependencies!=null)for(int i=0;i<dependencies.length();i++)checkBinary(dependencies.getJSONObject(i));
        JSONObject recipe=new JSONObject().put("profile","P11_LMA_RECIPE_DESCRIPTOR_1").put("recipe_id",bundle.recipeId)
                .put("architecture",bundle.architecture).put("pipeline",bundle.pipeline).put("backend",bundle.backend)
                .put("engine",bundle.engine).put("parameter_schema",engine.schema(bundle).json()).put("input_schema",engine.inputs(bundle,type).json());
        return new JSONObject().put("descriptor_profile","P11_LMA_CAPABILITY_DESCRIPTOR_1").put("bundle_id",bundle.id)
                .put("bundle_sha256",MeshJournal.sha(MeshJson.canonical(bundle.json()).getBytes(StandardCharsets.UTF_8)))
                .put("recipe_sha256",MeshJournal.sha(MeshJson.canonical(recipe).getBytes(StandardCharsets.UTF_8)))
                .put("engine_id",bundle.engine).put("engine_build_sha256",build.getString("sha256"))
                .put("backend",bundle.backend).put("task_type",type).put("parameter_schema",engine.schema(bundle).json())
                .put("input_schema",engine.inputs(bundle,type).json()).put("installed",true).put("market_qualified",false);
    }
    private void checkBinary(JSONObject file)throws Exception{
        String name=file.getString("file");if(!name.matches("lib[A-Za-z0-9_.-]+\\.so"))throw new IOException("MESH_ENGINE_FILE");
        File binary=new File(app.getApplicationInfo().nativeLibraryDir,name);
        if(!binary.isFile()||!DataFiles.sha256(binary).equals(file.getString("sha256")))throw new IOException("MESH_ENGINE_PIN_MISMATCH");
    }
    public void matches(com.lma.app.mesh.MeshLease lease)throws Exception{
        JSONObject d=describe(lease.bundle,lease.task);
        if(!d.getString("bundle_sha256").equals(lease.bundleHash)||!d.getString("recipe_sha256").equals(lease.recipeHash)||!d.getString("engine_id").equals(lease.engine)||!d.getString("engine_build_sha256").equals(lease.engineBuild)||!d.getString("backend").equals(lease.backend))throw new IOException("MESH_LOCAL_KERNEL_APPROVAL_MISMATCH");
    }
}
