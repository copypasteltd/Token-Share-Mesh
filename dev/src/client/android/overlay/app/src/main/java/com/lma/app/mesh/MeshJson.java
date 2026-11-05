package com.lma.app.mesh;

import android.util.JsonReader;
import android.util.JsonToken;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.IOException;
import java.io.StringReader;
import java.math.BigDecimal;
import java.nio.ByteBuffer;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.util.HashSet;
import java.util.Set;

/** Bounded strict UTF-8/JSON before any Android JSONObject last-value behavior. */
public final class MeshJson {
    private MeshJson() {}
    public static JSONObject parse(byte[] body, int maximum) throws Exception {
        if (body == null || body.length == 0 || body.length > maximum) throw new IOException("MESH_BODY_LIMIT");
        String text = StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
                .onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(body)).toString();
        try (JsonReader reader = new JsonReader(new StringReader(text))) {
            reader.setLenient(false); int[] count = {0}; Object value = read(reader, 0, count);
            if (!(value instanceof JSONObject) || reader.peek() != JsonToken.END_DOCUMENT) throw new IOException("MESH_JSON_OBJECT_REQUIRED");
            return (JSONObject)value;
        }
    }
    private static Object read(JsonReader r, int depth, int[] count) throws Exception {
        if (depth > 16 || ++count[0] > 4096) throw new IOException("MESH_JSON_LIMIT");
        switch (r.peek()) {
            case BEGIN_OBJECT:
                r.beginObject(); JSONObject object = new JSONObject(); Set<String> keys = new HashSet<>();
                while (r.hasNext()) {
                    String key = r.nextName();
                    if (key.length() > 128 || !keys.add(key)) throw new IOException("MESH_DUPLICATE_FIELD");
                    object.put(key, read(r, depth + 1, count));
                }
                r.endObject(); return object;
            case BEGIN_ARRAY:
                r.beginArray(); JSONArray array = new JSONArray();
                while (r.hasNext()) {
                    if (array.length() >= 128) throw new IOException("MESH_ARRAY_LIMIT");
                    array.put(read(r, depth + 1, count));
                }
                r.endArray(); return array;
            case STRING:
                String value = r.nextString();
                if (value.length() > 32768 || value.indexOf(0) >= 0) throw new IOException("MESH_STRING_LIMIT");
                return value;
            case NUMBER:
                String number = r.nextString();
                if (number.length() > 64) throw new IOException("MESH_NUMBER_LIMIT");
                BigDecimal decimal = new BigDecimal(number);
                if (Math.abs(decimal.scale()) > 32 || decimal.precision() > 32) throw new IOException("MESH_NUMBER_LIMIT");
                return decimal;
            case BOOLEAN: return r.nextBoolean();
            case NULL: r.nextNull(); return JSONObject.NULL;
            default: throw new IOException("MESH_JSON_INVALID");
        }
    }
    public static void fields(JSONObject object, String... names) throws Exception {
        Set<String> allowed = new HashSet<>(java.util.Arrays.asList(names));
        for (java.util.Iterator<String> it = object.keys(); it.hasNext();) if (!allowed.contains(it.next())) throw new IOException("MESH_UNKNOWN_FIELD");
    }
    public static String string(JSONObject object, String field) throws Exception {
        Object value = object.get(field);
        if (!(value instanceof String)) throw new IOException("MESH_STRING_REQUIRED");
        return (String)value;
    }
    public static long integer(JSONObject object, String field, long min, long max) throws Exception {
        Object value = object.get(field);
        if (!(value instanceof Number)) throw new IOException("MESH_INTEGER_REQUIRED");
        long number;
        try { number = new BigDecimal(value.toString()).longValueExact(); }
        catch (ArithmeticException e) { throw new IOException("MESH_INTEGER_REQUIRED"); }
        if (number < min || number > max) throw new IOException("MESH_INTEGER_LIMIT");
        return number;
    }
    public static JSONObject copy(JSONObject value) throws Exception {
        return parse(value.toString().getBytes(StandardCharsets.UTF_8), 65536);
    }
    /** Versioned descriptor digest only, never a replacement for the lease signature encoding. */
    public static String canonical(Object value) throws Exception {
        if(value instanceof JSONObject){
            JSONObject o=(JSONObject)value;java.util.List<String> keys=new java.util.ArrayList<>();
            for(java.util.Iterator<String> it=o.keys();it.hasNext();)keys.add(it.next());java.util.Collections.sort(keys);
            StringBuilder b=new StringBuilder("{");for(String k:keys){if(b.length()>1)b.append(',');b.append(JSONObject.quote(k)).append(':').append(canonical(o.get(k)));}return b.append('}').toString();
        }
        if(value instanceof JSONArray){JSONArray a=(JSONArray)value;StringBuilder b=new StringBuilder("[");for(int i=0;i<a.length();i++){if(i>0)b.append(',');b.append(canonical(a.get(i)));}return b.append(']').toString();}
        if(value instanceof String)return JSONObject.quote((String)value);
        if(value instanceof Number)return new BigDecimal(value.toString()).stripTrailingZeros().toPlainString();
        if(value instanceof Boolean)return value.toString();
        if(value==JSONObject.NULL)return "null";throw new IOException("MESH_CANONICAL_TYPE");
    }
}
