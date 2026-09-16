package io.jankhunter.okhttp3

import okhttp3.Response

internal interface HttpTransportObservation : HttpFirstByteObservation {
    fun armTransport(source: HttpResponseByteSource, streamId: Int)
    fun onFinalResponse(response: Response)
    fun onCachedBodyComplete(failureKind: Int)
}
