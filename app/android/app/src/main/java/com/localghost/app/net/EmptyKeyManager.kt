package com.localghost.app.net

import java.net.Socket
import java.security.Principal
import java.security.PrivateKey
import java.security.cert.X509Certificate
import javax.net.ssl.X509KeyManager

/** Presents no client certificate, for the enroll bootstrap call before a device cert is issued. */
object EmptyKeyManager : X509KeyManager {
    override fun chooseClientAlias(keyType: Array<out String>?, issuers: Array<out Principal>?, socket: Socket?): String? = null
    override fun getCertificateChain(alias: String?): Array<X509Certificate>? = null
    override fun getPrivateKey(alias: String?): PrivateKey? = null
    override fun getClientAliases(keyType: String?, issuers: Array<out Principal>?): Array<String>? = null
    override fun chooseServerAlias(keyType: String?, issuers: Array<out Principal>?, socket: Socket?): String? = null
    override fun getServerAliases(keyType: String?, issuers: Array<out Principal>?): Array<String>? = null
}
