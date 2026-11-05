package com.lma.app;

import com.lma.app.core.*;
import com.lma.app.mesh.*;
import org.json.JSONObject;
import java.io.IOException;

/** Public OpenAI-shaped values become the installed LMA recipe, without opaque vendor fallbacks. */
public final class MeshMediaRequests {
    private MeshMediaRequests(){}
    public static JSONObject normalize(LmaApp app,MeshLease lease,byte[] body)throws Exception{
        if(!MeshJournal.sha(body).equals(lease.requestPayloadHash))throw new IOException("MESH_ORIGINAL_REQUEST_BYTES");
        JSONObject raw=MeshJson.parse(body,65536),input,parameters;
        if(!MeshJson.string(raw,"model").equals(lease.modelAlias))throw new IOException("MESH_ORIGINAL_MODEL_ALIAS");
        if(lease.task.equals("image.generate")){
            JSONObject generation=MeshJson.copy(raw);
            if(lease.operation.equals("OPENAI_IMAGES_EDITS_V1")){generation.remove("image");if(raw.has("mask"))throw new IOException("MESH_MASK_CAPABILITY_UNDECLARED");}
            JSONObject mapped=OpenAiRequests.image(generation);
            if(mapped.optBoolean("allowUnmeasured")||mapped.has("adapters"))throw new IOException("MESH_UNAPPROVED_MEDIA_EXTENSION");
            input=mapped.getJSONObject("input");parameters=mapped.getJSONObject("parameters");
        }else if(lease.task.equals("video.generate")){
            // JSON represents the normalized control portion of the OpenAI
            // video job. Uploaded reference assets use private role bindings.
            MeshJson.fields(raw,"model","prompt","size","seconds","input_reference","lma");
            JSONObject options=raw.has("lma")?raw.getJSONObject("lma"):new JSONObject();MeshJson.fields(options,"parameters");
            parameters=options.has("parameters")?MeshJson.copy(options.getJSONObject("parameters")):new JSONObject();
            input=new JSONObject().put("prompt",MeshJson.string(raw,"prompt"));
            if(raw.has("size")){
                String size=MeshJson.string(raw,"size");if(!size.matches("[1-9][0-9]{0,3}x[1-9][0-9]{0,3}"))throw new IOException("MESH_VIDEO_SIZE");
                String[] shape=size.split("x");int width=Integer.parseInt(shape[0]),height=Integer.parseInt(shape[1]);
                if(parameters.has("width")&&parameters.getInt("width")!=width||parameters.has("height")&&parameters.getInt("height")!=height)throw new IOException("MESH_VIDEO_SIZE_CONFLICT");
                parameters.put("width",width).put("height",height);
            }
        }else throw new IOException("MESH_MEDIA_TASK_UNSUPPORTED");
        if(input.has("artifacts")||input.has("transcripts"))throw new IOException("MESH_PUBLIC_LMA_HISTORY_REFERENCE_FORBIDDEN");
        java.util.Set<String> referenced=new java.util.HashSet<>();
        if(lease.operation.equals("OPENAI_IMAGES_EDITS_V1"))referenced.add(fileReference(raw.get("image")));
        if(lease.operation.equals("OPENAI_VIDEOS_V1")&&raw.has("input_reference"))referenced.add(fileReference(raw.get("input_reference")));
        org.json.JSONArray selected=new org.json.JSONArray();
        for(MeshLease.InputAsset asset:lease.inputs){if(!referenced.remove(asset.id))throw new IOException("MESH_UNREFERENCED_SIGNED_ASSET");selected.put(asset.id);}
        if(!referenced.isEmpty())throw new IOException("MESH_ORIGINAL_REFERENCE_ASSET_REQUIRED");
        if(selected.length()>0)input.put("artifacts",selected);
        String prompt=input.getString("prompt");if(prompt.trim().isEmpty()||prompt.length()>16000||prompt.indexOf(0)>=0)throw new IOException("MESH_PROMPT_LIMIT");
        JSONObject normalized=app.bundles.parameters(lease.bundle).normalize(app.bundles.settings(lease.bundle).getJSONObject("values"),parameters);
        if(normalized.getJSONArray("adjustments").length()!=0)throw new IOException("MESH_RECIPE_PARAMETERS_WOULD_CHANGE");
        JSONObject values=normalized.getJSONObject("values");
        long width=MeshJson.integer(values,"width",1,lease.maxWidth),height=MeshJson.integer(values,"height",1,lease.maxHeight);
        if(Math.multiplyExact(width,height)>Math.multiplyExact(lease.maxWidth,lease.maxHeight))throw new IOException("MESH_MEDIA_PIXEL_LIMIT");
        if(lease.task.equals("video.generate")){
            long frames=MeshJson.integer(values,"frames",1,lease.maxFrames),fps=MeshJson.integer(values,"fps",1,120);
            if(frames>Math.multiplyExact(lease.maxSeconds,fps)+1)throw new IOException("MESH_VIDEO_DURATION_LIMIT");
            if(raw.has("seconds")){
                Object seconds=raw.get("seconds");long requested;
                if(seconds instanceof String&&((String)seconds).matches("[1-9][0-9]{0,3}"))requested=Long.parseLong((String)seconds);
                else requested=MeshJson.integer(raw,"seconds",1,lease.maxSeconds);
                if(requested>lease.maxSeconds||Math.multiplyExact(requested,fps)!=frames-1)throw new IOException("MESH_EXPLICIT_VIDEO_FRAME_MAPPING_REQUIRED");
            }
        }
        JSONObject request=new JSONObject().put("bundleId",lease.bundle).put("type",lease.task).put("input",input).put("parameters",values).put("allowUnmeasured",false);
        return TaskInput.validate(request);
    }
    private static String fileReference(Object value)throws Exception{
        if(!(value instanceof JSONObject))throw new IOException("MESH_NORMALIZED_OPENAI_FILE_ID_REQUIRED");
        JSONObject file=(JSONObject)value;MeshJson.fields(file,"file_id");String id=MeshJson.string(file,"file_id");
        if(!id.matches("[A-Za-z][A-Za-z0-9_-]{2,127}"))throw new IOException("MESH_REFERENCE_ID");return id;
    }
}
