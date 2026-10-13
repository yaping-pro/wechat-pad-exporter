package com.wx.sniffer.ipc

import android.net.LocalSocket
import android.net.LocalSocketAddress
import android.util.Log
import org.json.JSONObject
import java.io.BufferedWriter
import java.io.OutputStreamWriter
import java.nio.charset.StandardCharsets
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/**
 * RingBufferSender handles high-throughput, non-blocking asynchronous IPC
 * streaming from the WeChat process to the Go daemon over Linux Abstract UDS.
 */
object RingBufferSender {
    private const val TAG = "WX-RingBufferSender"
    private const val SOCKET_NAME = "wechat_stream_ipc"
    private const val QUEUE_CAPACITY = 8192

    private val queue = ArrayBlockingQueue<String>(QUEUE_CAPACITY)
    private val isRunning = AtomicBoolean(false)
    private var workerThread: Thread? = null

    @Volatile
    private var isConnected = false

    fun start() {
        if (isRunning.compareAndSet(false, true)) {
            workerThread = Thread({ runLoop() }, "wx-ipc-sender-thread").apply {
                isDaemon = true
                start()
            }
            Log.i(TAG, "RingBufferSender started background worker")
        }
    }

    fun stop() {
        isRunning.set(false)
        workerThread?.interrupt()
        workerThread = null
    }

    /**
     * Enqueue a raw JSON message. Returns true if queued, false if buffer full (never blocks caller).
     */
    fun enqueue(json: String): Boolean {
        if (!isRunning.get()) {
            start()
        }
        val offered = queue.offer(json)
        if (!offered) {
            Log.w(TAG, "RingBuffer queue full! Message dropped to preserve WeChat responsiveness.")
        }
        return offered
    }

    /**
     * Helper to construct and enqueue a hook message.
     */
    fun sendHookPayload(
        msgId: Long,
        msgSvrId: Long,
        talker: String,
        displayName: String = "",
        type: Int,
        subType: Int = 0,
        isSend: Int,
        content: String,
        createTime: Long,
        selfWxid: String = ""
    ): Boolean {
        return try {
            val json = JSONObject().apply {
                put("msg_id", msgId)
                put("msg_svr_id", msgSvrId)
                put("talker", talker)
                put("display_name", displayName)
                put("type", type)
                put("sub_type", subType)
                put("is_send", isSend)
                put("content", content)
                put("create_time", createTime)
                put("self_wxid", selfWxid)
            }.toString()
            enqueue(json)
        } catch (e: Exception) {
            Log.e(TAG, "Failed to build JSON payload: ${e.message}")
            false
        }
    }

    private fun runLoop() {
        while (isRunning.get()) {
            var socket: LocalSocket? = null
            var writer: BufferedWriter? = null

            try {
                Log.i(TAG, "Connecting to abstract UDS @$SOCKET_NAME...")
                socket = LocalSocket()
                val address = LocalSocketAddress(SOCKET_NAME, LocalSocketAddress.Namespace.ABSTRACT)
                socket.connect(address)
                socket.sendBufferSize = 64 * 1024
                writer = BufferedWriter(OutputStreamWriter(socket.outputStream, StandardCharsets.UTF_8))

                isConnected = true
                Log.i(TAG, "Successfully connected to daemon IPC socket @$SOCKET_NAME")

                while (isRunning.get()) {
                    // Poll with timeout so we can check isRunning
                    val message = queue.poll(1000, TimeUnit.MILLISECONDS) ?: continue
                    writer.write(message)
                    writer.newLine()
                    writer.flush()
                }
            } catch (e: InterruptedException) {
                break
            } catch (e: Exception) {
                Log.w(TAG, "IPC Connection error: ${e.message}. Retrying in 2000ms...")
                isConnected = false
            } finally {
                isConnected = false
                try {
                    writer?.close()
                } catch (_: Exception) {}
                try {
                    socket?.close()
                } catch (_: Exception) {}
            }

            if (isRunning.get()) {
                try {
                    Thread.sleep(2000)
                } catch (e: InterruptedException) {
                    break
                }
            }
        }
        Log.i(TAG, "RingBufferSender loop stopped")
    }
}
