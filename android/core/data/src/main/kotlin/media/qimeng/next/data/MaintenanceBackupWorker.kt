package media.qimeng.next.data

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.ServiceInfo
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.work.CoroutineWorker
import androidx.work.ForegroundInfo
import androidx.work.WorkerParameters
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

class MaintenanceBackupWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val graph = DataGraph.get(applicationContext)
        val target = graph.settings.session.value ?: return@withContext Result.success()
        if (target.instanceId != inputData.getString("instance")) return@withContext Result.success()
        try {
            val manager = applicationContext.getSystemService(NotificationManager::class.java)
            manager.createNotificationChannel(NotificationChannel("maintenance-backup", "资料备份", NotificationManager.IMPORTANCE_LOW))
            val notice = NotificationCompat.Builder(applicationContext, "maintenance-backup").setSmallIcon(android.R.drawable.stat_sys_upload)
                .setContentTitle("绮梦资料备份").setContentText("正在读取检查点并核验维护目录").setOngoing(true).build()
            setForeground(if (Build.VERSION.SDK_INT >= 29) ForegroundInfo(7002, notice, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC) else ForegroundInfo(7002, notice))
            if (graph.backups.advance(target, inputData.getBoolean("manual", false))) Result.success() else Result.retry()
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) {
            graph.backups.failure(target, when (failure) {
                is SecurityException -> "系统未授予此目录读写权限，请重新选择目录"
                is java.io.FileNotFoundException -> "维护目录中的文件不可用，请核对系统授权"
                else -> safeFailure(failure)
            })
            if (failure is java.io.IOException && (failure !is ApiFailure || failure.status >= 500) && runAttemptCount < 4) Result.retry() else Result.failure()
        }
    }
}
