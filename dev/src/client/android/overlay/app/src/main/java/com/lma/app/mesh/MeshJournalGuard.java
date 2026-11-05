package com.lma.app.mesh;

import java.io.File;
import java.io.IOException;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.channels.OverlappingFileLockException;
import java.nio.file.LinkOption;
import java.nio.file.StandardOpenOption;

/** OS-owned journal lifetime lock; a second app instance never reads stale FREE. */
final class MeshJournalGuard implements AutoCloseable {
    private final FileChannel channel;
    private final FileLock lock;
    private MeshJournalGuard(FileChannel channel,FileLock lock){this.channel=channel;this.lock=lock;}
    static MeshJournalGuard acquire(File directory)throws Exception{
        File file=new File(directory,"owner.lock");
        if(!file.getCanonicalFile().getParentFile().equals(directory.getCanonicalFile()))throw new IOException("MESH_JOURNAL_LOCK_PATH");
        FileChannel channel=FileChannel.open(file.toPath(),StandardOpenOption.CREATE,StandardOpenOption.WRITE,LinkOption.NOFOLLOW_LINKS);
        try{
            FileLock lock;
            try{lock=channel.tryLock();}catch(OverlappingFileLockException busy){throw new IOException("MESH_JOURNAL_ALREADY_OPEN");}
            if(lock==null)throw new IOException("MESH_JOURNAL_ALREADY_OPEN");return new MeshJournalGuard(channel,lock);
        }catch(Exception error){channel.close();throw error;}
    }
    @Override public void close()throws IOException{try{if(lock.isValid())lock.release();}finally{channel.close();}}
}
