package com.lma.app;

import android.Manifest;
import android.app.*;
import android.content.*;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.os.*;
import android.view.*;
import android.widget.*;
import com.lma.app.mesh.MeshJson;
import com.lma.app.mesh.MeshJournal;
import com.lma.app.mesh.MeshDeviceIdentity;
import org.json.JSONObject;
import java.io.*;
import java.util.Arrays;
import java.util.concurrent.*;

/** NB04 local owner controls. No media previews, prompt views, broad storage permission or remote bridge. */
public final class MeshProviderActivity extends Activity {
    private static final int IMPORT_APPROVAL=701,NOTIFICATIONS=702;
    private final Handler main=new Handler(Looper.getMainLooper());
    private final ExecutorService files=Executors.newSingleThreadExecutor(r->new Thread(r,"Mesh-owner-import"));
    private MeshProviderController controller;
    private TextView status,identity;
    private LinearLayout body;
    private boolean bound,destroyed;
    private final ServiceConnection connection=new ServiceConnection(){
        @Override public void onServiceConnected(ComponentName name,IBinder binder){
            if(!(binder instanceof MeshServiceBinder)){show("本机控制服务暂不可用");return;}
            controller=((MeshServiceBinder)binder).controller();refresh();
        }
        @Override public void onServiceDisconnected(ComponentName name){controller=null;show("服务已断开；原任务占用不会自动解除");}
    };
    private final Runnable poll=new Runnable(){@Override public void run(){if(destroyed)return;refresh();main.postDelayed(this,1000);}};
    @Override public void onCreate(Bundle state){
        super.onCreate(state);getWindow().setStatusBarColor(Color.BLACK);getWindow().setNavigationBarColor(Color.BLACK);
        ScrollView scroll=new ScrollView(this);scroll.setBackgroundColor(Color.rgb(246,244,234));body=new LinearLayout(this);body.setOrientation(LinearLayout.VERTICAL);body.setPadding(dp(22),dp(28),dp(22),dp(30));scroll.addView(body);setContentView(scroll);
        text("TOKEN SHARE MESH",15,true);text("让闲置\n开始创造收益。",34,true);
        text(Build.MANUFACTURER+" "+Build.MODEL,18,true);
        text("你决定何时共享、使用哪些已安装模型。客户的提示词和生成内容不在这里展示。",15,false);
        status=text("正在连接本机服务…",17,true);style(status,Color.rgb(217,255,102));
        button("01  创建设备公钥",Color.rgb(255,218,75),()->withControl(c->observe(c.initializeIdentity(),key->{identity.setText("设备公钥\n"+key);((ClipboardManager)getSystemService(CLIPBOARD_SERVICE)).setPrimaryClip(ClipData.newPlainText("设备公钥",key));show("设备公钥已复制，可用于绑定本人设备");})));
        identity=text("公钥用于设备绑定，私钥保留在本机。",13,false);
        button("02  导入本人批准的配置",Color.WHITE,()->{Intent intent=new Intent(Intent.ACTION_OPEN_DOCUMENT).setType("application/json").addCategory(Intent.CATEGORY_OPENABLE);startActivityForResult(intent,IMPORT_APPROVAL);});
        button("03  开启本机共享准备",Color.rgb(217,255,102),()->{
            if(Build.VERSION.SDK_INT>=33&&checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)!=PackageManager.PERMISSION_GRANTED){requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS},NOTIFICATIONS);return;}
            if(!getSystemService(NotificationManager.class).areNotificationsEnabled()){show("请先允许通知，以便随时查看和停止共享");return;}
            withControl(c->observe(c.enable(),value->show("本机已准备；市场接单仍需通过设备资格核验")));
        });
        button("暂停共享",Color.rgb(255,151,183),()->withControl(c->observe(c.disable(),value->show("正在停止接单并处理原任务"))));
        button("重新提交原执行回执",Color.WHITE,()->withControl(c->observe(c.retryReports(),value->show("已处理可恢复的原执行回执"))));
        button("重试原任务清理",Color.WHITE,()->withControl(c->observe(c.retryOwnedCleanup(),value->show("原任务已清理，请重新检查共享条件"))));
        button("设备重启后恢复",Color.WHITE,()->withControl(c->observe(c.recoverAfterReboot(),value->show("已恢复原设备占用记录，请重新开启共享"))));
        button("打开本机模型工作台",Color.rgb(162,201,255),()->startActivity(new Intent(this,MainActivity.class)));
        text("计费单位：小时 / M token\n图片和视频只展示任务状态，不公开生成内容。",13,false);
        Intent service=new Intent(this,LmaService.class).setAction("MESH_CONTROL_START");
        startForegroundService(service);bound=bindService(new Intent(this,LmaService.class).setAction("MESH_LOCAL_CONTROL"),connection,Context.BIND_AUTO_CREATE);main.post(poll);
    }
    private interface ControlAction{void run(MeshProviderController controller);}
    private void withControl(ControlAction action){if(controller==null){show("本机服务尚未连接");return;}action.run(controller);}
    private interface Success<T>{void accept(T value);}
    private <T> void observe(CompletableFuture<T> future,Success<T> success){future.whenComplete((value,error)->main.post(()->{if(destroyed)return;if(error!=null){show(friendly(error));return;}success.accept(value);refresh();}));}
    private void refresh(){if(controller==null||status==null)return;JSONObject state=controller.status();String phase=state.optString("state");
        String label="DISABLED".equals(phase)?"共享已关闭":"APPROVED".equals(phase)?"配置已批准":"READY".equals(phase)?"本机已准备":"EXECUTING".equals(phase)?"正在执行原任务":"QUARANTINED".equals(phase)?"原任务需要恢复":"正在处理";
        status.setText(label+"\n"+state.optString("model_name")+"\n"+(state.optBoolean("market_qualified")?"设备资格已确认":"市场接单尚未开放"));
    }
    @Override protected void onActivityResult(int request,int result,Intent data){
        super.onActivityResult(request,result,data);if(request!=IMPORT_APPROVAL||result!=RESULT_OK||data==null)return;Uri uri=data.getData();
        if(uri==null||!"content".equals(uri.getScheme())){show("请使用系统文件选择器选择配置文件");return;}
        files.execute(()->{byte[] raw=null;try(InputStream input=getContentResolver().openInputStream(uri);ByteArrayOutputStream output=new ByteArrayOutputStream()){
            if(input==null)throw new IOException("MESH_CONFIG_IMPORT");byte[] buffer=new byte[4096];int count;while((count=input.read(buffer))!=-1){if(output.size()+count>65536)throw new IOException("MESH_CONFIG_IMPORT_LIMIT");output.write(buffer,0,count);}raw=output.toByteArray();JSONObject config=MeshJson.parse(raw,65536),approval=config.getJSONObject("approval");
            String issuer=MeshJournal.sha(MeshDeviceIdentity.decoded(config.getString("issuer_public_key"),32));
            String message="请确认这是你本人选择的配置，并从可信渠道核对签发公钥指纹。\n\n服务："+config.getString("control_origin")+"\n模型："+approval.getString("bundle_id")+"\n后端："+approval.getString("backend")+"\n签发公钥指纹：\n"+issuer;
            final byte[] chosen=raw;raw=null;main.post(()->{
                if(destroyed){Arrays.fill(chosen,(byte)0);return;}
                new AlertDialog.Builder(this).setTitle("确认本人批准的配置").setMessage(message).setPositiveButton("已核对，批准",(dialog,which)->{
                    if(controller==null){Arrays.fill(chosen,(byte)0);show("本机服务尚未连接");return;}
                    CompletableFuture<JSONObject> approved=controller.importApproval(chosen);Arrays.fill(chosen,(byte)0);observe(approved,value->show("已保存本人批准的内核与共享条件"));
                }).setNegativeButton("取消",(dialog,which)->Arrays.fill(chosen,(byte)0)).setOnCancelListener(dialog->Arrays.fill(chosen,(byte)0)).show();
            });
        }catch(Exception error){main.post(()->{if(!destroyed)show("配置未导入，请检查文件是否完整");});}finally{if(raw!=null)Arrays.fill(raw,(byte)0);}});
    }
    private String friendly(Throwable error){
        Throwable cause=error;while(cause.getCause()!=null)cause=cause.getCause();String code=cause.getMessage();
        if(code!=null&&code.contains("REBOOT_REQUIRED"))return "请先重启手机，再恢复原任务；不会强行清空占用";
        if(code!=null&&(code.contains("DISABLE_BEFORE")||code.contains("DISABLE_AND_CLEAN")))return "请先暂停共享，并完成原任务清理";
        if(code!=null&&code.contains("NOT_INITIALIZED"))return "请先创建设备公钥，再导入本人批准的配置";
        if(code!=null&&code.contains("APPROVAL_MISMATCH"))return "已安装的模型或内核与批准配置不符，请重新核对";
        if(code!=null&&code.contains("HISTORY_LIMIT"))return "本机回执空间已满，请先完成原回执处理";
        return "操作未完成，请检查配置、共享条件和原任务状态";
    }
    private TextView text(String value,int size,boolean bold){TextView view=new TextView(this);view.setText(value);view.setTextSize(size);view.setTextColor(Color.BLACK);if(bold)view.setTypeface(Typeface.DEFAULT,Typeface.BOLD);LinearLayout.LayoutParams params=new LinearLayout.LayoutParams(-1,-2);params.bottomMargin=dp(18);view.setLayoutParams(params);body.addView(view);return view;}
    private void button(String title,int color,Runnable action){Button button=new Button(this);button.setText(title);button.setTextColor(Color.BLACK);button.setTextSize(16);button.setTypeface(Typeface.DEFAULT,Typeface.BOLD);button.setAllCaps(false);style(button,color);LinearLayout.LayoutParams params=new LinearLayout.LayoutParams(-1,dp(60));params.bottomMargin=dp(14);button.setLayoutParams(params);button.setOnClickListener(view->action.run());body.addView(button);}
    private void style(TextView view,int color){GradientDrawable background=new GradientDrawable();background.setColor(color);background.setStroke(dp(3),Color.BLACK);view.setBackground(background);view.setPadding(dp(14),dp(12),dp(14),dp(12));}
    private int dp(int value){return Math.round(value*getResources().getDisplayMetrics().density);}
    private void show(String text){if(!destroyed)Toast.makeText(this,text,Toast.LENGTH_LONG).show();}
    @Override protected void onDestroy(){destroyed=true;main.removeCallbacksAndMessages(null);if(bound)unbindService(connection);files.shutdownNow();super.onDestroy();}
}
